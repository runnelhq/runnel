// Package pipeline runs pipelines (KONZEPT, chapter 6.2): every transform runs in its
// own goroutine, every link is a buffered channel of record batches. Full buffers block
// the transform before them (backpressure), so memory stays constant however many
// records flow. A transform with several incoming links reads them as one stream; a
// transform with several outgoing links passes every record to each of them (copy).
package pipeline

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/engine/runid"
	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/vars"
)

// Defaults of Options (KONZEPT, chapter 6.2); configurable per pipeline with roadmap S17.
const (
	DefaultBatchSize  = 500
	DefaultBufferSize = 10000
)

// Options configure a run.
type Options struct {
	Registry *transform.Registry
	// Scope resolves ${…} references in the configuration of the transforms.
	Scope *vars.Scope
	// Environment is the name of the selected environment, for run.environment in expressions.
	Environment string
	// Observer receives the events of the run; it is called by one goroutine at a time.
	Observer func(Event)
	// BatchSize is the number of records passed over a link at once.
	BatchSize int
	// BufferSize is the number of records a link holds before the transform before it waits.
	BufferSize int
	// RunID is the id of the run; if empty, Run creates one.
	RunID string
	// Now returns the current time; defaults to time.Now.
	Now func() time.Time
	// Rows are records passed to the run, read by core.from-result (S24a).
	Rows []record.Record
	// Respond answers the waiting caller of an http trigger (http.response, S38c); nil if none.
	Respond action.Responder
}

// Status is the final state of a run or a transform.
type Status string

// States.
const (
	Succeeded Status = "succeeded"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

// Counts are the numbers of records of a transform.
type Counts struct {
	// Read is the number of records the transform received.
	Read int64 `json:"read"`
	// Written is the number of records the transform passed on.
	Written int64 `json:"written"`
	// Errors is the number of records the transform passed to its error links.
	Errors int64 `json:"errors,omitempty"`
}

// Result is the outcome of a run.
type Result struct {
	RunID    string
	Status   Status
	Err      error
	Duration time.Duration
	// Counts by transform id.
	Counts map[string]Counts
	// LinkRows are the numbers of records passed over each link, by index in the
	// list of links (S25a3).
	LinkRows []int64
	// Rows are the result records of the run, written by core.to-result (S24a).
	Rows []record.Record
}

// EventKind classifies events of a run.
type EventKind string

// Event kinds.
const (
	RunStarted        EventKind = "run.started"
	RunFinished       EventKind = "run.finished"
	TransformStarted  EventKind = "transform.started"
	TransformFinished EventKind = "transform.finished"
	Log               EventKind = "log"
)

// Event is something that happened during a run.
type Event struct {
	Time          time.Time
	Kind          EventKind
	RunID         string
	TransformID   string // empty for run events
	TransformType string
	Level         slog.Level // for Log
	Message       string
	Status        Status        // TransformFinished, RunFinished
	Err           error         // TransformFinished, RunFinished
	Duration      time.Duration // TransformFinished, RunFinished
	Counts        Counts        // TransformFinished
	// LinkRows are the numbers of records passed over the outgoing links of the
	// transform, by link index (TransformFinished).
	LinkRows map[int]int64
}

// Runner runs a prepared pipeline.
type Runner struct {
	p          *model.Pipeline
	opts       Options
	transforms []transform.Transform
	// copies holds the further instances of transforms with copies > 1.
	copies [][]transform.Transform
	// schemas holds the schema of the output of each transform (ADR 0007).
	schemas []record.Schema
	// errSchemas holds the schema of the records each transform passes to error links.
	errSchemas []record.Schema
	// inputs holds the merged schema of the incoming records of each transform; nil
	// for a transform without incoming links.
	inputs   []*record.Schema
	warnings model.Errors
	emitMu   sync.Mutex
}

// Prepare creates the transforms of a pipeline: their types must exist and their
// configuration must be valid. The pipeline must have been validated (Pipeline.Validate)
// without errors. All problems are returned as model.Errors.
func Prepare(p *model.Pipeline, opts Options) (*Runner, error) {
	r, errs := build(p, opts)
	if err := errs.Err(); err != nil {
		return nil, err
	}
	diags := r.deriveSchemas()
	for i, l := range p.Links {
		if l.Type != model.ErrorLink {
			continue
		}
		for _, t := range p.Transforms {
			if def, _ := opts.Registry.Lookup(t.Type); t.ID == l.From && !def.RecordErrors {
				diags = append(diags, p.WarningAt(fmt.Sprintf("$.links[%d].type", i),
					"link #%d (%s → %s): %s does not pass records with errors on; the error link is never used", i+1, l.From, l.To, t.Type))
			}
		}
	}
	if diags.HasErrors() {
		return nil, diags.Err()
	}
	diags.Sort()
	r.warnings = diags
	return r, nil
}

// build creates the transforms of a pipeline; a transform that cannot be created
// (unknown type, invalid configuration) stays nil and its problem is returned.
func build(p *model.Pipeline, opts Options) (*Runner, model.Errors) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = cmp.Or(p.Settings.BatchSize, DefaultBatchSize)
	}
	if opts.BufferSize <= 0 {
		opts.BufferSize = cmp.Or(p.Settings.BufferSize, DefaultBufferSize)
	}
	r := &Runner{p: p, opts: opts, transforms: make([]transform.Transform, len(p.Transforms)),
		copies: make([][]transform.Transform, len(p.Transforms))}
	var errs model.Errors
	for i, t := range p.Transforms {
		path := fmt.Sprintf("$.transforms[%d]", i)
		def, ok := opts.Registry.Lookup(t.Type)
		if !ok {
			errs = append(errs, p.ErrorAt(path+".type", "transform %q: unknown transform type %q (available: %v)",
				t.ID, t.Type, opts.Registry.Types()))
			continue
		}
		with, err := expand(t.With, opts.Scope)
		if err == nil {
			r.transforms[i], err = def.New(asText(with, opts.Registry, t.Type))
		}
		if err != nil {
			errs = append(errs, p.ErrorAt(path+".with", "transform %q (%s): %v", t.ID, t.Type, err))
			continue
		}
		if t.Copies > 1 && !def.Parallel {
			errs = append(errs, p.ErrorAt(path+".copies", "transform %q: %s cannot run in several copies (it would read or write the same data more than once)", t.ID, t.Type))
			continue
		}
		// Further copies are separate instances of the same configuration.
		for range t.Copies - 1 {
			c, err := def.New(asText(with, opts.Registry, t.Type))
			if err != nil {
				errs = append(errs, p.ErrorAt(path+".with", "transform %q (%s): %v", t.ID, t.Type, err))
				break
			}
			r.copies[i] = append(r.copies[i], c)
		}
	}
	return r, errs
}

// TransformSchemas are the schemas around one transform, for the editor (roadmap S25a2).
type TransformSchemas struct {
	// Input is the merged schema of the incoming records; missing for a transform
	// without incoming links.
	Input *record.Schema `json:"input,omitempty"`
	// Output is the schema of the records on its data links.
	Output record.Schema `json:"output"`
	// Errors is the schema of the records on its error links (with _error, _errorField
	// and _errorTransform).
	Errors record.Schema `json:"errors"`
}

// Schemas derives the schemas of all transforms by id, like Prepare does. Unlike
// Prepare, it also works when some transforms have errors: their records count as
// unknown (an open schema). The pipeline must pass Pipeline.Validate without errors.
func Schemas(p *model.Pipeline, opts Options) map[string]TransformSchemas {
	r, _ := build(p, opts)
	_ = r.deriveSchemas() // the problems are those of Check
	out := make(map[string]TransformSchemas, len(p.Transforms))
	for i, t := range p.Transforms {
		if _, dup := out[t.ID]; !dup {
			out[t.ID] = TransformSchemas{Input: r.inputs[i], Output: r.schemas[i], Errors: r.errSchemas[i]}
		}
	}
	return out
}

// outLink is an outgoing link of a transform.
type outLink struct {
	ch     chan transform.Batch
	index  int    // index of the link in the pipeline
	to     int    // index of the transform it leads to
	target string // its id
}

// port is a group of outgoing links that get the same records (e.g. all data links,
// the error links, or the links to one target), with the batch being collected for them.
type port struct {
	links []outLink
	batch transform.Batch
}

// values returns what expressions see besides the record (ADR 0006): vars, params, env
// and run. They are the same for all transforms and records of a run.
func (r *Runner) values(runID string, started time.Time) map[string]any {
	scope := r.opts.Scope
	if scope == nil {
		scope = model.PipelineScope(r.p, nil, nil, nil)
	}
	variables, params := model.ExprValues(scope, r.p.Parameters)
	return map[string]any{
		"vars":   variables,
		"params": params,
		"env":    model.Environ(),
		"run":    map[string]any{"id": runID, "pipeline": r.p.Name, "environment": r.opts.Environment, "startedAt": started},
	}
}

// checkStrict reports links whose schema is open, which the strict mode does not allow
// (ADR 0007): once per transform for its data links, and per error link.
func (r *Runner) checkStrict() model.Errors {
	errSchemas := r.errSchemas
	p := r.p
	index := map[string]int{}
	for i, t := range p.Transforms {
		index[t.ID] = i
	}
	var diags model.Errors
	reported := map[int]bool{}
	for li, l := range p.Links {
		from := index[l.From]
		if l.Type == model.ErrorLink {
			if errSchemas[from].Open {
				diags = append(diags, p.ErrorAt(fmt.Sprintf("$.links[%d]", li),
					"link #%d (%s → %s): the fields of the records with errors are not all known (settings.strict); %s has no input with known fields", li+1, l.From, l.To, l.From))
			}
			continue
		}
		if r.schemas[from].Open && !reported[from] {
			reported[from] = true
			diags = append(diags, p.ErrorAt(fmt.Sprintf("$.transforms[%d]", from),
				"transform %q: the fields of its records are not all known, but settings.strict requires it (e.g. list them with fields:)", l.From))
		}
	}
	return diags
}

// Warnings returns the warnings found by Prepare, e.g. fields that are not known in an open schema.
func (r *Runner) Warnings() model.Errors { return r.warnings }

// Schema returns the schema of the records a transform emits.
func (r *Runner) Schema(transformID string) (record.Schema, bool) {
	for i, t := range r.p.Transforms {
		if t.ID == transformID {
			return r.schemas[i], true
		}
	}
	return record.Schema{}, false
}

// deriveSchemas calls Init of the transforms in the order of the links (the graph has
// no cycles): each gets the merged schemas of the transforms before it (ADR 0007).
func (r *Runner) deriveSchemas() model.Errors {
	p := r.p
	index := map[string]int{}
	for i, t := range p.Transforms {
		index[t.ID] = i
	}
	type input struct {
		from  int
		error bool // an error link: the records the transform could not process
	}
	inputs := make([][]input, len(p.Transforms))
	pending := make([]int, len(p.Transforms))
	next := make([][]int, len(p.Transforms))
	targets := make([][]string, len(p.Transforms))
	for _, l := range p.Links {
		from, to := index[l.From], index[l.To]
		inputs[to] = append(inputs[to], input{from, l.Type == model.ErrorLink})
		if l.Type != model.ErrorLink && !slices.Contains(targets[from], l.To) {
			targets[from] = append(targets[from], l.To)
		}
		next[from] = append(next[from], to)
		pending[to]++
	}
	var queue []int
	for i := range p.Transforms {
		if pending[i] == 0 {
			queue = append(queue, i)
		}
	}
	r.schemas = make([]record.Schema, len(p.Transforms))
	r.errSchemas = make([]record.Schema, len(p.Transforms))
	r.inputs = make([]*record.Schema, len(p.Transforms))
	errSchemas := r.errSchemas
	var diags model.Errors
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		var in []record.Schema
		for _, x := range inputs[i] {
			if x.error {
				in = append(in, errSchemas[x.from])
			} else {
				in = append(in, r.schemas[x.from])
			}
		}
		// On error links: the incoming records with the error fields; for a transform
		// without input (e.g. csv.input) the values as read, so nothing is known.
		errSchemas[i] = record.OpenSchema()
		if len(in) > 0 {
			merged := record.Merge(in...)
			r.inputs[i] = &merged
			errSchemas[i] = record.Merge(in...).
				With(transform.ErrorField, record.String).
				With(transform.ErrorFieldField, record.String).
				With(transform.ErrorTransformField, record.String)
		}
		t := p.Transforms[i]
		r.schemas[i] = record.OpenSchema()
		if init, ok := r.transforms[i].(transform.Initializer); ok {
			ic := transform.NewInitContext(record.Merge(in...))
			ic.Targets = targets[i]
			ic.Strict = p.Settings.Strict
			out, err := init.Init(ic)
			// The copies get the same Init; their problems are those of the first one.
			for _, c := range r.copies[i] {
				if ci, ok := c.(transform.Initializer); ok {
					cic := transform.NewInitContext(record.Merge(in...))
					cic.Targets, cic.Strict = targets[i], ic.Strict
					_, _ = ci.Init(cic)
				}
			}
			base := fmt.Sprintf("$.transforms[%d].with", i)
			for _, pr := range ic.Problems() {
				path := base
				if pr.Path != "" {
					path += "." + pr.Path
				}
				if pr.Warning {
					diags = append(diags, p.WarningAt(path, "transform %q: %s", t.ID, pr.Message))
				} else {
					diags = append(diags, p.ErrorAt(path, "transform %q: %s", t.ID, pr.Message))
				}
			}
			if err != nil {
				diags = append(diags, p.ErrorAt(base, "transform %q (%s): %v", t.ID, t.Type, err))
			} else {
				r.schemas[i] = out
			}
		}
		for _, to := range next[i] {
			if pending[to]--; pending[to] == 0 {
				queue = append(queue, to)
			}
		}
	}
	if p.Settings.Strict {
		diags = append(diags, r.checkStrict()...)
	}
	return diags
}

// Check returns the problems Prepare finds – errors and warnings –, e.g. for "runnel validate".
func Check(p *model.Pipeline, opts Options) model.Errors {
	r, err := Prepare(p, opts)
	var diags model.Errors
	if errors.As(err, &diags) {
		return diags
	}
	if err != nil {
		return model.Errors{p.ErrorAt("$", "%v", err)}
	}
	return r.Warnings()
}

// asText writes numbers and booleans as text where the schema of the transform expects
// text, e.g. "rows: 100" for a field that may also hold "${ROWS}" (like the workflow engine).
func asText(with map[string]any, reg *transform.Registry, typ string) map[string]any {
	d, ok := reg.Descriptor(typ)
	if !ok || d.Schema == nil || with == nil {
		return with
	}
	out := maps.Clone(with)
	for _, p := range d.Schema.Properties {
		switch v := out[p.Name].(type) {
		case string, nil, map[string]any, []any:
		default:
			if p.Schema.Type == "string" {
				out[p.Name] = expr.Format(v)
			}
		}
	}
	return out
}

// expand resolves ${…} references in the configuration; without a scope it stays as it is.
func expand(v map[string]any, scope *vars.Scope) (map[string]any, error) {
	if scope == nil || v == nil {
		return v, nil
	}
	out, err := expandValue(v, scope)
	if err != nil {
		return nil, err
	}
	return out.(map[string]any), nil
}

func expandValue(v any, scope *vars.Scope) (any, error) {
	switch v := v.(type) {
	case string:
		return scope.Expand(v)
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			x, err := expandValue(e, scope)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			out[k] = x
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			x, err := expandValue(e, scope)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			out[i] = x
		}
		return out, nil
	}
	return v, nil
}

func (r *Runner) emit(e Event) {
	if r.opts.Observer != nil {
		r.emitMu.Lock()
		defer r.emitMu.Unlock()
		r.opts.Observer(e)
	}
}

// Run executes the pipeline and returns when all transforms have finished.
func (r *Runner) Run(parent context.Context) Result {
	now := r.opts.Now
	started := now()
	runID := r.opts.RunID
	if runID == "" {
		runID = runid.New(started)
	}
	r.emit(Event{Time: started, Kind: RunStarted, RunID: runID, Message: r.p.Name})
	values := r.values(runID, started)
	var resultMu sync.Mutex
	var results []record.Record
	addResult := func(rec record.Record) error {
		resultMu.Lock()
		defer resultMu.Unlock()
		results = append(results, rec)
		return nil
	}

	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)

	// One channel per link; the buffer holds BufferSize records in batches.
	capacity := max(1, r.opts.BufferSize/r.opts.BatchSize)
	links := make([]chan transform.Batch, len(r.p.Links))
	for i := range links {
		links[i] = make(chan transform.Batch, capacity)
	}
	index := map[string]int{}
	for i, t := range r.p.Transforms {
		index[t.ID] = i
	}
	ins := make([][]int, len(r.p.Transforms))
	outs := make([][]int, len(r.p.Transforms))
	for i, l := range r.p.Links {
		outs[index[l.From]] = append(outs[index[l.From]], i)
		ins[index[l.To]] = append(ins[index[l.To]], i)
	}

	counts := make([]struct{ read, written, errors atomic.Int64 }, len(r.p.Transforms))
	linkRows := make([]atomic.Int64, len(r.p.Links))
	var failMu sync.Mutex
	var failure error
	fail := func(err error) {
		failMu.Lock()
		if failure == nil {
			failure = err
		}
		failMu.Unlock()
		cancel(err)
	}

	var wg sync.WaitGroup
	for i, t := range r.p.Transforms {
		in := r.input(ctx, links, ins[i], &wg)
		// The outgoing links of the transform: data links and error links.
		var dataLinks, errLinks []outLink
		for _, li := range outs[i] {
			l := r.p.Links[li]
			ol := outLink{ch: links[li], index: li, to: index[l.To], target: l.To}
			if l.Type == model.ErrorLink {
				errLinks = append(errLinks, ol)
			} else {
				dataLinks = append(dataLinks, ol)
			}
		}
		instances := append([]transform.Transform{r.transforms[i]}, r.copies[i]...)
		distribute := t.Distribution == model.DistributeRoundRobin && len(dataLinks) > 1
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := now()
			r.emit(Event{Time: start, Kind: TransformStarted, RunID: runID, TransformID: t.ID, TransformType: t.Type})

			// send passes a batch to every link of a port; links after the first get copies.
			send := func(p *port, b transform.Batch) error {
				for k, l := range p.links {
					batch := b
					if k > 0 {
						batch = make(transform.Batch, len(b))
						for j, rec := range b {
							batch[j] = rec.Clone()
						}
					}
					select {
					case l.ch <- batch:
						counts[l.to].read.Add(int64(len(b)))
						linkRows[l.index].Add(int64(len(b)))
					case <-ctx.Done():
						return context.Cause(ctx)
					}
				}
				return nil
			}
			// add collects records of a port into batches.
			add := func(p *port, rec record.Record) error {
				if err := ctx.Err(); err != nil {
					return context.Cause(ctx)
				}
				p.batch = append(p.batch, rec)
				if len(p.batch) < r.opts.BatchSize {
					return nil
				}
				full := p.batch
				p.batch = make(transform.Batch, 0, r.opts.BatchSize)
				return send(p, full)
			}
			log := func(level slog.Level, msg string) {
				r.emit(Event{Time: now(), Kind: Log, RunID: runID, TransformID: t.ID, TransformType: t.Type, Level: level, Message: msg})
			}

			// runCopy runs one instance; each has its own batches. The copies share the
			// incoming records and the outgoing links.
			runCopy := func(tr transform.Transform) error {
				all := &port{links: dataLinks}
				errs := &port{links: errLinks}
				ports := []*port{all, errs}
				single := make([]*port, len(dataLinks)) // one per data link, for distribute
				byTarget := map[string]*port{}          // the data links by target, for EmitTo
				for k, l := range dataLinks {
					single[k] = &port{links: []outLink{l}}
					ports = append(ports, single[k])
					if byTarget[l.target] == nil {
						byTarget[l.target] = &port{}
						ports = append(ports, byTarget[l.target])
					}
					byTarget[l.target].links = append(byTarget[l.target].links, l)
				}
				next := 0
				out := transform.NewOutput(t.ID, func(rec record.Record) error {
					counts[i].written.Add(1)
					if distribute {
						p := single[next]
						next = (next + 1) % len(single)
						return add(p, rec)
					}
					return add(all, rec)
				}, log).WithValues(values).WithResults(r.opts.Rows, addResult).WithRespond(r.opts.Respond)
				if len(errLinks) > 0 {
					out.WithErrorLinks(func(rec record.Record) error {
						counts[i].errors.Add(1)
						return add(errs, rec)
					})
				}
				out.WithTargets(func(target string, rec record.Record) error {
					p := byTarget[target]
					if p == nil {
						return fmt.Errorf("no data link leads to %q", target)
					}
					counts[i].written.Add(1)
					return add(p, rec)
				})
				err := process(ctx, tr, in, out)
				for _, p := range ports {
					if err == nil && len(p.batch) > 0 {
						err = send(p, p.batch)
					}
				}
				return err
			}

			results := make([]error, len(instances))
			var copiesWG sync.WaitGroup
			for k, tr := range instances {
				copiesWG.Add(1)
				go func() {
					defer copiesWG.Done()
					results[k] = runCopy(tr)
				}()
			}
			copiesWG.Wait()
			// The error of a copy that failed counts, not that of the others it stopped.
			var err error
			for _, e := range results {
				if e != nil && (err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.Cause(ctx))) {
					err = e
				}
			}

			// A failure stops the others first; draining the input afterwards ends quickly.
			status := Succeeded
			switch {
			case err != nil && ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.Cause(ctx))):
				status = Cancelled // stopped because another transform failed or the run was cancelled
			case err != nil:
				status = Failed
				err = fmt.Errorf("transform %q: %w", t.ID, err)
				fail(err)
			}
			for _, l := range append(dataLinks, errLinks...) {
				close(l.ch)
			}
			// Records still arriving are discarded, so that the transforms before can finish.
			for range in {
			}
			c := Counts{Read: counts[i].read.Load(), Written: counts[i].written.Load(), Errors: counts[i].errors.Load()}
			rows := make(map[int]int64, len(outs[i]))
			for _, li := range outs[i] {
				rows[li] = linkRows[li].Load()
			}
			end := now()
			r.emit(Event{Time: end, Kind: TransformFinished, RunID: runID, TransformID: t.ID, TransformType: t.Type,
				Status: status, Err: err, Duration: end.Sub(start), Counts: c, LinkRows: rows})
		}()
	}
	wg.Wait()

	res := Result{RunID: runID, Status: Succeeded, Duration: now().Sub(started), Counts: map[string]Counts{}, Rows: results,
		LinkRows: make([]int64, len(linkRows))}
	for i := range linkRows {
		res.LinkRows[i] = linkRows[i].Load()
	}
	for i, t := range r.p.Transforms {
		res.Counts[t.ID] = Counts{Read: counts[i].read.Load(), Written: counts[i].written.Load(), Errors: counts[i].errors.Load()}
	}
	switch {
	case failure != nil:
		res.Status, res.Err = Failed, failure
	case parent.Err() != nil:
		res.Status, res.Err = Cancelled, fmt.Errorf("run cancelled: %w", context.Cause(parent))
	}
	r.emit(Event{Time: now(), Kind: RunFinished, RunID: runID, Status: res.Status, Err: res.Err, Duration: res.Duration})
	return res
}

// process runs a transform; a panic becomes an error instead of ending the program.
func process(ctx context.Context, t transform.Transform, in <-chan transform.Batch, out *transform.Output) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("internal error: %v", p)
		}
	}()
	return t.Process(ctx, in, out)
}

// input returns the channel a transform reads: closed for none, the link's channel for
// one, and for several a channel that merges them (order between them is not defined).
func (r *Runner) input(ctx context.Context, links []chan transform.Batch, in []int, wg *sync.WaitGroup) <-chan transform.Batch {
	switch len(in) {
	case 0:
		ch := make(chan transform.Batch)
		close(ch)
		return ch
	case 1:
		return links[in[0]]
	}
	merged := make(chan transform.Batch, max(1, r.opts.BufferSize/r.opts.BatchSize))
	var inputs sync.WaitGroup
	for _, li := range in {
		inputs.Add(1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer inputs.Done()
			for b := range links[li] {
				select {
				case merged <- b:
				case <-ctx.Done():
					for range links[li] { // let the transform before finish
					}
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		inputs.Wait()
		close(merged)
	}()
	return merged
}

// ExpandConfig resolves the ${…} references in the configuration of a transform as
// Prepare does, e.g. for "Detect fields" in the editor (S46).
func ExpandConfig(with map[string]any, scope *vars.Scope) (map[string]any, error) {
	return expand(with, scope)
}
