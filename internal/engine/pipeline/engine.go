// Package pipeline runs pipelines (KONZEPT, chapter 6.2): every transform runs in its
// own goroutine, every link is a buffered channel of record batches. Full buffers block
// the transform before them (backpressure), so memory stays constant however many
// records flow. A transform with several incoming links reads them as one stream; a
// transform with several outgoing links passes every record to each of them (copy).
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"sync/atomic"
	"time"

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
}

// Runner runs a prepared pipeline.
type Runner struct {
	p          *model.Pipeline
	opts       Options
	transforms []transform.Transform
	// schemas holds the schema of the output of each transform (ADR 0007).
	schemas  []record.Schema
	warnings model.Errors
	emitMu   sync.Mutex
}

// Prepare creates the transforms of a pipeline: their types must exist and their
// configuration must be valid. The pipeline must have been validated (Pipeline.Validate)
// without errors. All problems are returned as model.Errors.
func Prepare(p *model.Pipeline, opts Options) (*Runner, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = DefaultBatchSize
	}
	if opts.BufferSize <= 0 {
		opts.BufferSize = DefaultBufferSize
	}
	r := &Runner{p: p, opts: opts, transforms: make([]transform.Transform, len(p.Transforms))}
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
		}
	}
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

// port is one kind of outgoing links of a transform (data or error) with the batch
// being collected for them.
type port struct {
	targets   []chan transform.Batch
	targetIdx []int
	batch     transform.Batch
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
	for _, l := range p.Links {
		from, to := index[l.From], index[l.To]
		inputs[to] = append(inputs[to], input{from, l.Type == model.ErrorLink})
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
	errSchemas := make([]record.Schema, len(p.Transforms))
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
			errSchemas[i] = record.Merge(in...).
				With(transform.ErrorField, record.String).
				With(transform.ErrorFieldField, record.String).
				With(transform.ErrorTransformField, record.String)
		}
		t := p.Transforms[i]
		r.schemas[i] = record.OpenSchema()
		if init, ok := r.transforms[i].(transform.Initializer); ok {
			ic := transform.NewInitContext(record.Merge(in...))
			out, err := init.Init(ic)
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
		data, errs := &port{}, &port{}
		for _, li := range outs[i] {
			p := data
			if r.p.Links[li].Type == model.ErrorLink {
				p = errs
			}
			p.targets = append(p.targets, links[li])
			p.targetIdx = append(p.targetIdx, index[r.p.Links[li].To])
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := now()
			r.emit(Event{Time: start, Kind: TransformStarted, RunID: runID, TransformID: t.ID, TransformType: t.Type})

			// send passes a batch to every link of a port; links after the first get copies.
			send := func(p *port, b transform.Batch) error {
				for k, ch := range p.targets {
					batch := b
					if k > 0 {
						batch = make(transform.Batch, len(b))
						for j, rec := range b {
							batch[j] = rec.Clone()
						}
					}
					select {
					case ch <- batch:
						counts[p.targetIdx[k]].read.Add(int64(len(b)))
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
			emitRecord := func(rec record.Record) error {
				counts[i].written.Add(1)
				return add(data, rec)
			}
			failRecord := func(rec record.Record) error {
				counts[i].errors.Add(1)
				return add(errs, rec)
			}
			log := func(level slog.Level, msg string) {
				r.emit(Event{Time: now(), Kind: Log, RunID: runID, TransformID: t.ID, TransformType: t.Type, Level: level, Message: msg})
			}
			out := transform.NewOutput(t.ID, emitRecord, log).WithValues(values)
			if len(errs.targets) > 0 {
				out.WithErrorLinks(failRecord)
			}

			err := process(ctx, r.transforms[i], in, out)
			for _, p := range []*port{data, errs} {
				if err == nil && len(p.batch) > 0 {
					err = send(p, p.batch)
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
			for _, ch := range append(data.targets, errs.targets...) {
				close(ch)
			}
			// Records still arriving are discarded, so that the transforms before can finish.
			for range in {
			}
			c := Counts{Read: counts[i].read.Load(), Written: counts[i].written.Load(), Errors: counts[i].errors.Load()}
			end := now()
			r.emit(Event{Time: end, Kind: TransformFinished, RunID: runID, TransformID: t.ID, TransformType: t.Type,
				Status: status, Err: err, Duration: end.Sub(start), Counts: c})
		}()
	}
	wg.Wait()

	res := Result{RunID: runID, Status: Succeeded, Duration: now().Sub(started), Counts: map[string]Counts{}}
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
