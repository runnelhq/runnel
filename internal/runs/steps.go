package runs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/schema"
	"github.com/runnelhq/runnel/internal/vars"
)

// maxAttemptErrors is the number of errors of earlier attempts kept per step.
const maxAttemptErrors = 10

// StepData is the data of one execution of an action in the last run started in the
// editor (E23a): what it got and returned. Secrets are masked; fields for secrets show
// their reference.
type StepData struct {
	RunID    string `json:"runId"`
	ActionID string `json:"actionId"`
	Type     string `json:"type"`
	// Run is the number of the execution shown (loops, join each), from 1; Runs how many
	// executions there are.
	Run    int             `json:"run"`
	Runs   int             `json:"runs"`
	Status workflow.Status `json:"status"`
	// Reason tells why a skipped action did not run.
	Reason     string `json:"reason,omitempty"`
	DurationMs int64  `json:"durationMs"`
	// InputSources are the texts from the file of the fields of the input that held
	// ${…} or {{ … }}, by path.
	InputSources map[string]string `json:"inputSources,omitempty"`
	// Entry is the entry of forEach shown (from 1; ?entry=), with its status; the other
	// fields then are those of the entry.
	Entry       int             `json:"entry,omitempty"`
	EntryStatus workflow.Status `json:"entryStatus,omitempty"`
	StepIO
	// Entries are the entries of forEach with their status (skipped: not run).
	Entries []EntryState `json:"entries,omitempty"`
}

// StepIO is what one execution or entry of forEach got and returned.
type StepIO struct {
	// Attempts counts the attempts (retry); the data is that of the last one, and
	// AttemptErrors are the errors of the earlier ones (the first 10).
	Attempts      int      `json:"attempts,omitempty"`
	AttemptErrors []string `json:"attemptErrors,omitempty"`
	// Row is the entry of forEach (row in expressions).
	Row any `json:"row,omitempty"`
	// Input is the configuration (with:) as it reached the action; Secrets lists its
	// fields for secrets, which hold the reference (${secret:…}) or ***.
	Input      map[string]any  `json:"input,omitempty"`
	Secrets    []string        `json:"secrets,omitempty"`
	Connection *StepConnection `json:"connection,omitempty"`
	Output     any             `json:"output,omitempty"`
	Error      string          `json:"error,omitempty"`
	Truncated  []Truncation    `json:"truncated,omitempty"`
	// Dropped is set when the run had kept MaxStepDataBytes already: only status and
	// error are kept.
	Dropped bool `json:"dropped,omitempty"`
	size    int
}

// StepConnection is the connection of a step as resolved for the environment of the run.
type StepConnection struct {
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	With    map[string]any `json:"with"`
	Secrets []string       `json:"secrets,omitempty"`
}

// EntryState is the status of an entry of forEach.
type EntryState struct {
	Index  int             `json:"index"`
	Status workflow.Status `json:"status"`
}

// StepSummary tells which actions have data (GET /runs/{id}/steps).
type StepSummary struct {
	ActionID string          `json:"actionId"`
	Type     string          `json:"type"`
	Runs     int             `json:"runs"`
	Status   workflow.Status `json:"status"`
	Entries  int             `json:"entries,omitempty"`
}

// stepRecorder keeps the data of the steps of one run in memory (workflow.StepRecorder).
type stepRecorder struct {
	reg     *action.Registry
	mask    *vars.Mask
	resolve connection.Resolver
	project *model.Project
	order   []string // action ids in the order of the file

	mu      sync.Mutex
	used    int
	execs   map[string][]*stepExec
	sources map[string]map[string]string // by action id
	conns   map[string]*StepConnection   // by name; nil: cannot be shown
	types   *connection.Registry
}

type stepExec struct {
	r        *stepRecorder
	a        *model.Action
	status   workflow.Status
	reason   string
	duration time.Duration
	io       StepIO
	forEach  bool
	entries  []entryData
}

type entryData struct {
	status workflow.Status
	io     StepIO
}

func newStepRecorder(w *model.Workflow, reg *action.Registry, mask *vars.Mask, resolve connection.Resolver, p *model.Project) *stepRecorder {
	r := &stepRecorder{reg: reg, mask: mask, resolve: resolve, project: p, execs: map[string][]*stepExec{},
		sources: map[string]map[string]string{}, conns: map[string]*StepConnection{}}
	for _, a := range w.Actions {
		r.order = append(r.order, a.ID)
	}
	return r
}

func (r *stepRecorder) Begin(a *model.Action) workflow.StepTrace {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := &stepExec{r: r, a: a, status: StatusRunning}
	r.execs[a.ID] = append(r.execs[a.ID], e)
	return e
}

func (r *stepRecorder) Skipped(a *model.Action, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.execs[a.ID] = append(r.execs[a.ID], &stepExec{r: r, a: a, status: workflow.Skipped, reason: reason})
}

func (e *stepExec) Entries(n int) {
	e.r.mu.Lock()
	defer e.r.mu.Unlock()
	e.forEach = true
	e.entries = make([]entryData, n)
	for i := range e.entries {
		e.entries[i].status = workflow.Skipped
	}
}

func (e *stepExec) Attempt(entry int, row any, attempt int, with map[string]any, res action.Result, err error) {
	r := e.r
	io := r.build(e.a, with, true, res.Output, err, row, entry > 0)
	io.Attempts = attempt
	r.mu.Lock()
	defer r.mu.Unlock()
	target := &e.io
	if entry > 0 && entry <= len(e.entries) {
		target = &e.entries[entry-1].io
		e.entries[entry-1].status = StatusRunning
	}
	if attempt > 1 {
		io.AttemptErrors = target.AttemptErrors
		if len(io.AttemptErrors) < maxAttemptErrors {
			io.AttemptErrors = append(slices.Clone(io.AttemptErrors), target.Error)
		}
	}
	r.store(target, io)
}

func (e *stepExec) Entry(entry int, status workflow.Status) {
	e.r.mu.Lock()
	defer e.r.mu.Unlock()
	if entry > 0 && entry <= len(e.entries) {
		e.entries[entry-1].status = status
	}
}

func (e *stepExec) End(status workflow.Status, d time.Duration, res action.Result, err error) {
	r := e.r
	if err == nil && status == workflow.Failed && res.Message != "" {
		err = errors.New(res.Message) // e.g. core.abort
	}
	r.mu.Lock()
	whole := e.forEach || e.io.Attempts == 0
	r.mu.Unlock()
	var io StepIO
	if whole { // forEach: the list of the entries; no attempt: only the error
		io = r.build(e.a, nil, false, res.Output, err, nil, false)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e.status, e.duration = status, d
	if whole {
		r.store(&e.io, io)
	} else {
		e.io.Error = limitError(err, r.mask)
	}
}

// store replaces the data at target with io, within MaxStepDataBytes. r.mu must be held.
func (r *stepRecorder) store(target *StepIO, io StepIO) {
	r.used -= target.size
	if r.used+io.size > MaxStepDataBytes {
		io = StepIO{Attempts: io.Attempts, AttemptErrors: io.AttemptErrors, Error: io.Error, Dropped: true}
		io.size = len(io.Error)
	}
	r.used += io.size
	*target = io
}

// left returns the bytes a part of a step may take.
func (r *stepRecorder) left() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return min(MaxStepBytes, MaxStepDataBytes-r.used)
}

// build copies what an action got and returned: secrets masked, within the limits.
func (r *stepRecorder) build(a *model.Action, with map[string]any, hasInput bool, output any, err error, row any, hasRow bool) StepIO {
	io := StepIO{Error: limitError(err, r.mask)}
	io.size = 64 + len(io.Error)
	part := func(name string, v any) any {
		budget := r.left()
		if budget <= 0 {
			io.Dropped = true
			return nil
		}
		out, n, cuts := limitValue(v, name, budget, r.mask)
		io.size += n
		io.Truncated = append(io.Truncated, cuts...)
		return out
	}
	if hasRow {
		io.Row = part("row", row)
	}
	if hasInput && with != nil {
		in, secrets := r.input(a, with)
		io.Input, _ = part("input", in).(map[string]any)
		io.Secrets = secrets
		io.Connection = r.connection(a.Type, with)
	}
	if output != nil {
		io.Output = part("output", output)
	}
	return io
}

// formats returns the formats of the fields of an action type, e.g. runnel-secret.
func (r *stepRecorder) formats(typ string) map[string]string {
	out := map[string]string{}
	if d, ok := r.reg.Descriptor(typ); ok && d.Schema != nil {
		for _, p := range d.Schema.Properties {
			if p.Schema.Format != "" {
				out[p.Name] = p.Schema.Format
			}
		}
	}
	return out
}

// input returns the configuration of an action with each field for a secret replaced
// by its text in the file (the reference), and the paths of those fields.
func (r *stepRecorder) input(a *model.Action, with map[string]any) (map[string]any, []string) {
	in := maps.Clone(with)
	var secrets []string
	for field, format := range r.formats(a.Type) {
		v, ok := in[field]
		if !ok {
			continue
		}
		switch format {
		case schema.FormatSecret:
			in[field] = secretRef(a.With[field])
			if m, isMap := v.(map[string]any); isMap { // e.g. env of shell.run
				for name := range m {
					secrets = append(secrets, field+"."+name)
				}
			} else {
				secrets = append(secrets, field)
			}
		case schema.FormatParams: // values of a script: those with ${secret:…}
			raw, _ := a.With[field].(map[string]any)
			m, isMap := v.(map[string]any)
			if !isMap {
				continue
			}
			out := maps.Clone(m)
			for name, x := range raw {
				if s, isText := x.(string); isText && strings.Contains(s, "${secret:") {
					out[name] = s
					secrets = append(secrets, field+"."+name)
				}
			}
			in[field] = out
		}
	}
	slices.Sort(secrets)
	return in, secrets
}

// secretRef returns the text of a field for a secret from the file if it refers to the
// secret (${…}); otherwise *** – the value never leaves the server. A mapping (env of
// shell.run, headers of http) is replaced value by value.
func secretRef(raw any) any {
	switch v := raw.(type) {
	case string:
		if strings.Contains(v, "${") {
			return v
		}
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			out[k] = secretRef(x)
		}
		return out
	}
	return vars.Masked
}

// sourcesOf returns the texts from the file of the fields of an action that hold ${…} or
// {{ … }}, by path; fields for secrets, SQL and scripts are left out.
func (r *stepRecorder) sourcesOf(a *model.Action) map[string]string {
	r.mu.Lock()
	src, ok := r.sources[a.ID]
	r.mu.Unlock()
	if ok {
		return src
	}
	src = map[string]string{}
	formats := r.formats(a.Type)
	for field, v := range a.With {
		switch formats[field] {
		case schema.FormatSecret, schema.FormatSQL, schema.FormatScript:
			continue
		}
		addSources(src, field, v)
	}
	if len(src) == 0 {
		src = nil
	}
	r.mu.Lock()
	r.sources[a.ID] = src
	r.mu.Unlock()
	return src
}

func addSources(src map[string]string, path string, v any) {
	switch v := v.(type) {
	case string:
		if strings.Contains(v, "${secret:") {
			return
		}
		if strings.Contains(v, "${") || strings.Contains(v, "{{") {
			src[path] = v
		}
	case map[string]any:
		for k, x := range v {
			addSources(src, keyPath(path, k), x)
		}
	case []any:
		for i, x := range v {
			addSources(src, fmt.Sprintf("%s[%d]", path, i), x)
		}
	}
}

// connection returns the connection an action uses, resolved for the environment of the
// run, with its fields for secrets replaced by their reference; nil if it has none.
func (r *stepRecorder) connection(typ string, with map[string]any) *StepConnection {
	if r.project == nil || r.resolve == nil {
		return nil
	}
	var name string
	for field, format := range r.formats(typ) {
		if s, ok := with[field].(string); ok && format == schema.FormatConnection && s != "" {
			name = s
			break
		}
	}
	if name == "" {
		return nil
	}
	r.mu.Lock()
	c, ok := r.conns[name]
	if r.types == nil {
		r.types = connectionTypes(r.project)
	}
	types := r.types
	r.mu.Unlock()
	if ok {
		return c
	}
	if file := r.project.Connections[name]; file != nil {
		if res, err := r.resolve(context.Background(), name); err == nil {
			c = &StepConnection{Name: name, Type: res.Type, With: map[string]any{}}
			for field, v := range res.With {
				if types.SecretField(res.Type, field) {
					c.With[field] = secretRef(file.With[field])
					c.Secrets = append(c.Secrets, field)
					continue
				}
				c.With[field], _, _ = limitValue(v, "connection", MaxStepBytes, r.mask)
			}
			slices.Sort(c.Secrets)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conns[name] = c
	return c
}

// data returns the data of execution run (from 1; 0: the last) of an action, or of
// one of its entries of forEach (from 1; 0: the whole step).
func (r *stepRecorder) data(actionID string, run, entry int) (StepData, error) {
	r.mu.Lock()
	execs := r.execs[actionID]
	if run == 0 {
		run = len(execs)
	}
	if run < 1 || run > len(execs) {
		r.mu.Unlock()
		return StepData{}, ErrNoStep
	}
	e := execs[run-1]
	d := StepData{ActionID: actionID, Type: e.a.Type, Run: run, Runs: len(execs), Status: e.status, Reason: e.reason,
		DurationMs: e.duration.Milliseconds(), StepIO: e.io}
	if e.forEach {
		d.Entries = make([]EntryState, len(e.entries))
		for i, en := range e.entries {
			d.Entries[i] = EntryState{Index: i + 1, Status: en.status}
		}
	}
	if entry != 0 {
		if entry < 1 || entry > len(e.entries) {
			r.mu.Unlock()
			return StepData{}, ErrNoStep
		}
		d.Entry, d.EntryStatus, d.StepIO = entry, e.entries[entry-1].status, e.entries[entry-1].io
	}
	r.mu.Unlock()
	if d.Input != nil {
		d.InputSources = r.sourcesOf(e.a)
	}
	return d, nil
}

// summary lists the actions with data, in the order of the file.
func (r *stepRecorder) summary() []StepSummary {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []StepSummary{}
	for _, id := range r.order {
		execs := r.execs[id]
		if len(execs) == 0 {
			continue
		}
		last := execs[len(execs)-1]
		out = append(out, StepSummary{ActionID: id, Type: last.a.Type, Runs: len(execs), Status: last.status, Entries: len(last.entries)})
	}
	return out
}

// ErrNoStepData is returned by StepData and Steps when no data is kept for the run: it
// was not the last run started in the editor, or it is a pipeline (E23a).
var ErrNoStepData = errors.New("no data kept for this run")

// ErrNoStep is returned by StepData when the run has no data for the action, execution
// or entry.
var ErrNoStep = errors.New("no data for this step")

// Steps lists the actions with data of the last run started in the editor (E23a).
func (m *Manager) Steps(runID string) ([]StepSummary, error) {
	rec := m.stepsOf(runID)
	if rec == nil {
		return nil, ErrNoStepData
	}
	return rec.summary(), nil
}

// StepData returns the data of an action in the last run started in the editor (E23a):
// execution run (from 1; 0: the last) and entry of forEach (from 1; 0: the whole step).
func (m *Manager) StepData(runID, actionID string, run, entry int) (StepData, error) {
	rec := m.stepsOf(runID)
	if rec == nil {
		return StepData{}, ErrNoStepData
	}
	d, err := rec.data(actionID, run, entry)
	d.RunID = runID
	return d, err
}

func (m *Manager) stepsOf(runID string) *stepRecorder {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.steps == nil || m.stepsRun != runID {
		return nil
	}
	return m.steps
}
