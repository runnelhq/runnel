package runs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/runnelhq/runnel/internal/engine/pipeline"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/model"
)

// Limits of the in-memory history. The SQLite history (roadmap S26) replaces it.
const (
	// MaxRuns is the number of runs kept; the oldest finished runs are dropped first.
	MaxRuns = 100
	// MaxHTTPRuns is the number of runs of http triggers kept besides them (S38b2), so
	// that many requests do not push out the runs started in the editor or by schedule.
	MaxHTTPRuns = 100
	// MaxLogLines is the number of log lines kept per run.
	MaxLogLines = 5000
	// MaxEvents is the number of events kept for clients that reconnect.
	MaxEvents = 2000
)

// States while a run or action is active; the final states are those of workflow.Status.
const (
	StatusRunning workflow.Status = "running"
	// StatusPending: the action has not started (yet).
	StatusPending workflow.Status = "pending"
)

// Run is the state of a run as shown by the API.
type Run struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	// Kind is "workflow" or "pipeline" (S25a3).
	Kind string `json:"kind"`
	// Workflow is the name of the workflow or pipeline.
	Workflow string `json:"workflow"`
	// Trigger names what started the run, e.g. "cron:nightly" (S37); empty: by hand.
	Trigger string `json:"trigger,omitempty"`
	// Client is the address of the caller of an http trigger (S38a).
	Client      string            `json:"client,omitempty"`
	Environment string            `json:"environment,omitempty"`
	Params      map[string]string `json:"params,omitempty"`
	Status      workflow.Status   `json:"status"`
	Started     time.Time         `json:"started"`
	Finished    *time.Time        `json:"finished,omitempty"`
	DurationMs  int64             `json:"durationMs"`
	Error       string            `json:"error,omitempty"`
	Outputs     map[string]any    `json:"outputs,omitempty"`
	// LinkRows are the records passed over each link of a pipeline, by link index;
	// a link counts once the transform before it has finished.
	LinkRows []int64 `json:"linkRows,omitempty"`
}

// Detail is a run with the state of its actions and its log.
type Detail struct {
	Run
	Actions []*ActionState `json:"actions"`
	Log     []LogLine      `json:"log"`
	// LogTruncated is set when older log lines were dropped (MaxLogLines).
	LogTruncated bool `json:"logTruncated,omitempty"`
}

// ActionState is the state of one action in a run.
type ActionState struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Status     workflow.Status `json:"status"`
	Started    *time.Time      `json:"started,omitempty"`
	Finished   *time.Time      `json:"finished,omitempty"`
	DurationMs int64           `json:"durationMs,omitempty"`
	Error      string          `json:"error,omitempty"`
	// Runs counts how often the action ran (loops, join: each).
	Runs int `json:"runs"`
	// Counts are the records a transform of a pipeline read, wrote and passed to its
	// error links; set when it has finished.
	Counts *pipeline.Counts `json:"counts,omitempty"`
}

// LogLine is a message of the run log.
type LogLine struct {
	Time     time.Time `json:"time"`
	Level    string    `json:"level"`
	ActionID string    `json:"actionId,omitempty"`
	Message  string    `json:"message"`
}

// Event is a change of a run, as sent to clients (server-sent events).
type Event struct {
	Seq        int64              `json:"seq"`
	Kind       workflow.EventKind `json:"kind"`
	Time       time.Time          `json:"time"`
	RunID      string             `json:"runId"`
	ActionID   string             `json:"actionId,omitempty"`
	ActionType string             `json:"actionType,omitempty"`
	Level      string             `json:"level,omitempty"`
	Message    string             `json:"message,omitempty"`
	Status     workflow.Status    `json:"status,omitempty"`
	DurationMs int64              `json:"durationMs,omitempty"`
	Error      string             `json:"error,omitempty"`
	Outputs    map[string]any     `json:"outputs,omitempty"`
	// Counts and LinkRows: a transform of a pipeline has finished (action.finished).
	Counts   *pipeline.Counts `json:"counts,omitempty"`
	LinkRows map[int]int64    `json:"linkRows,omitempty"`
}

// Manager starts runs and keeps their state and events in memory.
type Manager struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.Mutex
	runs    map[string]*entry
	order   []string // run ids, oldest first
	events  []Event  // the last MaxEvents events
	seq     int64
	waiters map[chan struct{}]struct{}
	// draining is set by Drain: no new runs are started.
	draining bool
}

type entry struct {
	run          Run
	actions      []*ActionState
	byID         map[string]*ActionState
	log          []LogLine
	logTruncated bool
	cancel       context.CancelFunc
	// history is all (default), errors (drop the run when it succeeded) or none (drop
	// it when it ends and publish no events), for runs of http triggers (S38b2).
	history string
	// done is closed when the run has ended (Wait).
	done chan struct{}
}

// NewManager creates a manager. Runs are cancelled by Shutdown.
func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{ctx: ctx, cancel: cancel, runs: map[string]*entry{}, waiters: map[chan struct{}]struct{}{}}
}

// ErrShutdown is returned by Start after Drain or Shutdown.
var ErrShutdown = errors.New("the server is shutting down")

// LimitError is returned by Start when a limit of runs at the same time is reached
// (settings.maxConcurrentRuns of the project or the workflow); the run is not started.
// A queue that starts it later is in the backlog.
type LimitError struct{ msg string }

func (e *LimitError) Error() string { return e.msg }

// limits are the limits of runs at the same time that apply to a start; 0: none.
type limits struct {
	project int // all runs of the server
	file    int // runs of the same file
}

func limitsOf(p *model.Project, perFile int) limits {
	l := limits{file: perFile}
	if p != nil {
		l.project = p.Settings.MaxConcurrentRuns
	}
	return l
}

// newID returns the id of a new run, or ErrShutdown when no runs are started anymore.
func (m *Manager) newID() (string, error) {
	if m.stopping() {
		return "", ErrShutdown
	}
	return workflow.NewRunID(time.Now()), nil
}

// Start prepares and starts a run in the background and returns its state. The
// spec's Observer and RunID are set by the manager. Errors are those of Prepare.
func (m *Manager) Start(path string, spec Spec) (Run, *Prepared, error) {
	id, err := m.newID()
	if err != nil {
		return Run{}, nil, err
	}
	spec.RunID = id
	spec.Observer = func(e workflow.Event) { m.record(id, e) }
	p, err := Prepare(spec)
	if err != nil {
		return Run{}, p, err
	}

	steps := make([][2]string, len(p.Workflow.Actions))
	for i, a := range p.Workflow.Actions {
		steps[i] = [2]string{a.ID, a.Type}
	}
	run, err := m.add(id, Run{ID: id, Path: path, Kind: "workflow", Workflow: p.Workflow.Name, Params: spec.Params, Trigger: spec.Trigger, Client: spec.Client},
		p.Env, steps, limitsOf(p.Project, p.Workflow.Settings.MaxConcurrentRuns), spec.History, func(ctx context.Context) { p.Runner.Run(ctx) })
	return run, p, err
}

// StartPipeline prepares and starts a pipeline run in the background, like Start (S25a3).
// Its transforms are reported as actions, with their counts when they have finished.
func (m *Manager) StartPipeline(path string, spec PipelineSpec) (Run, *PreparedPipeline, error) {
	id, err := m.newID()
	if err != nil {
		return Run{}, nil, err
	}
	spec.RunID = id
	spec.Observer = func(e pipeline.Event) { m.recordPipeline(id, e) }
	p, err := PreparePipeline(spec)
	if err != nil {
		return Run{}, p, err
	}
	steps := make([][2]string, len(p.Pipeline.Transforms))
	for i, t := range p.Pipeline.Transforms {
		steps[i] = [2]string{t.ID, t.Type}
	}
	run, err := m.add(id, Run{ID: id, Path: path, Kind: "pipeline", Workflow: p.Pipeline.Name, Params: spec.Params, Trigger: spec.Trigger, Client: spec.Client,
		LinkRows: make([]int64, len(p.Pipeline.Links))}, p.Env, steps, limitsOf(p.Project, 0),
		spec.History, func(ctx context.Context) { p.Runner.Run(ctx) })
	return run, p, err
}

// add registers a run with its steps (id and type of the actions or transforms) and
// runs it in the background, unless a limit of runs at the same time is reached.
func (m *Manager) add(id string, run Run, env *model.Environment, steps [][2]string, lim limits, history string, start func(context.Context)) (Run, error) {
	ctx, cancel := context.WithCancel(m.ctx)
	run.Status, run.Started = StatusRunning, time.Now()
	if env != nil {
		run.Environment = env.Name
	}
	e := &entry{run: run, byID: map[string]*ActionState{}, cancel: cancel, history: history, done: make(chan struct{})}
	for _, st := range steps {
		s := &ActionState{ID: st[0], Type: st[1], Status: StatusPending}
		e.actions = append(e.actions, s)
		e.byID[st[0]] = s
	}
	m.mu.Lock()
	if m.draining || m.ctx.Err() != nil {
		m.mu.Unlock()
		cancel()
		return Run{}, ErrShutdown
	}
	if err := m.checkLimits(run.Path, lim); err != nil {
		m.mu.Unlock()
		cancel()
		return Run{}, err
	}
	m.runs[id] = e
	m.order = append(m.order, id)
	m.prune()
	out := e.run
	out.LinkRows = slices.Clone(out.LinkRows) // the events change the entry's slice
	m.mu.Unlock()

	m.wg.Go(func() {
		defer cancel()
		start(ctx)
		m.mu.Lock()
		m.ended(e)
		m.mu.Unlock()
	})
	return out, nil
}

// checkLimits reports a LimitError if starting another run of path would exceed a
// limit. m.mu must be held.
func (m *Manager) checkLimits(path string, lim limits) error {
	all, same := 0, 0
	for _, e := range m.runs {
		if e.run.Status != StatusRunning {
			continue
		}
		all++
		if e.run.Path == path {
			same++
		}
	}
	switch {
	case lim.file > 0 && same >= lim.file:
		return &LimitError{fmt.Sprintf("%s is already running %s (settings.maxConcurrentRuns: %d); try again when a run has ended",
			path, times(same), lim.file)}
	case lim.project > 0 && all >= lim.project:
		return &LimitError{fmt.Sprintf("%s already running in the project (settings.maxConcurrentRuns in %s: %d); try again when a run has ended",
			times(all), model.ProjectFile, lim.project)}
	}
	return nil
}

func times(n int) string {
	if n == 1 {
		return "1 run"
	}
	return fmt.Sprintf("%d runs", n)
}

// pipelineKinds maps the events of a pipeline to those of a workflow: transforms are
// reported as actions, so that clients handle both alike.
var pipelineKinds = map[pipeline.EventKind]workflow.EventKind{
	pipeline.RunStarted:        workflow.RunStarted,
	pipeline.RunFinished:       workflow.RunFinished,
	pipeline.TransformStarted:  workflow.ActionStarted,
	pipeline.TransformFinished: workflow.ActionFinished,
	pipeline.Log:               workflow.Log,
}

// recordPipeline applies an event of the pipeline engine to the run state and publishes it.
func (m *Manager) recordPipeline(id string, pe pipeline.Event) {
	we := workflow.Event{Kind: pipelineKinds[pe.Kind], Time: pe.Time, RunID: pe.RunID, ActionID: pe.TransformID,
		ActionType: pe.TransformType, Level: pe.Level, Message: pe.Message, Status: workflow.Status(pe.Status),
		Err: pe.Err, Duration: pe.Duration}
	var counts *pipeline.Counts
	if pe.Kind == pipeline.TransformFinished {
		c := pe.Counts
		counts = &c
	}
	m.apply(id, we, counts, pe.LinkRows)
}

// prune drops the oldest finished runs beyond MaxRuns, and those of http triggers
// beyond MaxHTTPRuns. m.mu must be held.
func (m *Manager) prune() {
	for _, http := range []bool{false, true} {
		limit := MaxRuns
		if http {
			limit = MaxHTTPRuns
		}
		n := 0
		for _, id := range m.order {
			if isHTTP(m.runs[id]) == http {
				n++
			}
		}
		for i := 0; n > limit && i < len(m.order); {
			e := m.runs[m.order[i]]
			if isHTTP(e) != http || e.run.Status == StatusRunning {
				i++
				continue
			}
			m.remove(i)
			n--
		}
	}
}

func isHTTP(e *entry) bool { return strings.HasPrefix(e.run.Trigger, model.TriggerHTTP+":") }

// remove drops the run at position i of m.order. m.mu must be held.
func (m *Manager) remove(i int) {
	delete(m.runs, m.order[i])
	m.order = slices.Delete(m.order, i, i+1)
}

// Values of Spec.History (S38b2).
const (
	HistoryAll    = "all"
	HistoryErrors = "errors"
	HistoryNone   = "none"
)

// ended applies the history of a run that has ended. m.mu must be held.
func (m *Manager) ended(e *entry) {
	close(e.done)
	drop := e.history == HistoryNone || (e.history == HistoryErrors && e.run.Status == workflow.Succeeded)
	if !drop {
		return
	}
	if i := slices.Index(m.order, e.run.ID); i >= 0 {
		m.remove(i)
	}
}

// record applies an event of the engine to the run state and publishes it.
func (m *Manager) record(id string, we workflow.Event) { m.apply(id, we, nil, nil) }

// apply applies an event to the run state and publishes it; counts and rows are those
// of a finished transform of a pipeline.
func (m *Manager) apply(id string, we workflow.Event, counts *pipeline.Counts, rows map[int]int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.runs[id]
	if e == nil { // pruned (cannot happen while running) or event before registration
		return
	}
	ev := Event{Kind: we.Kind, Time: we.Time, RunID: id, ActionID: we.ActionID, ActionType: we.ActionType,
		Message: we.Message, Status: we.Status, DurationMs: we.Duration.Milliseconds()}
	if we.Err != nil {
		ev.Error = we.Err.Error()
	}
	a := e.byID[we.ActionID]
	switch we.Kind {
	case workflow.RunStarted:
		e.run.Started = we.Time
		ev.Message = e.run.Workflow
	case workflow.ActionStarted:
		if a != nil {
			t := we.Time
			a.Status, a.Started, a.Finished, a.DurationMs, a.Error = StatusRunning, &t, nil, 0, ""
			a.Runs++
		}
	case workflow.ActionWaiting:
		if a != nil {
			a.Status = workflow.Waiting
		}
	case workflow.ActionFinished, workflow.ActionSkipped:
		if a != nil {
			t := we.Time
			a.Finished = &t
			a.Status, a.DurationMs, a.Error = we.Status, ev.DurationMs, ev.Error
			if we.Kind == workflow.ActionSkipped {
				a.Status = workflow.Skipped
				ev.Status = workflow.Skipped
			}
			a.Counts = counts
		}
		ev.Counts, ev.LinkRows = counts, rows
		for li, n := range rows {
			if li >= 0 && li < len(e.run.LinkRows) {
				e.run.LinkRows[li] = n
			}
		}
	case workflow.Log:
		ev.Level = levelName(we.Level)
		if len(e.log) >= MaxLogLines {
			e.log = e.log[1:]
			e.logTruncated = true
		}
		e.log = append(e.log, LogLine{Time: we.Time, Level: ev.Level, ActionID: we.ActionID, Message: we.Message})
	case workflow.RunFinished:
		t := we.Time
		e.run.Status, e.run.Finished, e.run.DurationMs, e.run.Error = we.Status, &t, ev.DurationMs, ev.Error
		e.run.Outputs, ev.Outputs = we.Outputs, we.Outputs
		ev.Message = e.run.Workflow
	}
	if e.history != HistoryNone {
		m.publish(ev)
	}
}

// publish stores an event and wakes all subscribers. m.mu must be held.
func (m *Manager) publish(ev Event) {
	m.seq++
	ev.Seq = m.seq
	if len(m.events) >= MaxEvents {
		m.events = m.events[1:]
	}
	m.events = append(m.events, ev)
	for w := range m.waiters {
		close(w)
		delete(m.waiters, w)
	}
}

// EventsSince returns the events after seq. If events after seq were already dropped,
// complete is false and the client should reload the state of the runs. The returned
// channel is closed when newer events arrive.
func (m *Manager) EventsSince(seq int64) (events []Event, complete bool, wake <-chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	complete = true
	if len(m.events) > 0 && seq < m.events[0].Seq-1 {
		complete = false
	}
	for _, ev := range m.events {
		if ev.Seq > seq {
			events = append(events, ev)
		}
	}
	w := make(chan struct{})
	m.waiters[w] = struct{}{}
	return events, complete, w
}

// LastSeq returns the sequence number of the latest event.
func (m *Manager) LastSeq() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.seq
}

// Runs returns all runs, newest first.
func (m *Manager) Runs() []Run {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Run, 0, len(m.order))
	for _, id := range slices.Backward(m.order) {
		r := m.runs[id].run
		r.LinkRows = slices.Clone(r.LinkRows)
		out = append(out, r)
	}
	return out
}

// Get returns a run with its actions and log.
func (m *Manager) Get(id string) (Detail, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.runs[id]
	if e == nil {
		return Detail{}, false
	}
	return detailOf(e), true
}

// detailOf returns the state of a run. m.mu must be held.
func detailOf(e *entry) Detail {
	d := Detail{Run: e.run, Log: slices.Clone(e.log), LogTruncated: e.logTruncated}
	d.LinkRows = slices.Clone(e.run.LinkRows)
	for _, a := range e.actions {
		c := *a
		d.Actions = append(d.Actions, &c)
	}
	if d.Log == nil {
		d.Log = []LogLine{}
	}
	return d
}

// Wait waits until a run has ended or ctx is done and returns its state; false if
// there is no such run. It also returns the state of a run that history drops when it
// ends, if it was running when Wait was called.
func (m *Manager) Wait(ctx context.Context, id string) (Detail, bool) {
	m.mu.Lock()
	e := m.runs[id]
	m.mu.Unlock()
	if e == nil {
		return Detail{}, false
	}
	select {
	case <-e.done:
	case <-ctx.Done():
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return detailOf(e), true
}

// IsRunning reports whether a run is running, without copying its state like Get.
func (m *Manager) IsRunning(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.runs[id]
	return e != nil && e.run.Status == StatusRunning
}

// Cancel stops a running run. It reports whether the run exists and whether it was
// still running.
func (m *Manager) Cancel(id string) (found, running bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.runs[id]
	if e == nil {
		return false, false
	}
	if e.run.Status != StatusRunning {
		return true, false
	}
	e.cancel()
	return true, true
}

func (m *Manager) stopping() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.draining || m.ctx.Err() != nil
}

// Running returns the number of runs that are running.
func (m *Manager) Running() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, e := range m.runs {
		if e.run.Status == StatusRunning {
			n++
		}
	}
	return n
}

// Drain stops starting runs (Start returns ErrShutdown) and waits until the running
// runs have ended or ctx is done; it does not cancel them. Shutdown cancels the rest.
func (m *Manager) Drain(ctx context.Context) error {
	m.mu.Lock()
	m.draining = true
	m.mu.Unlock()
	return m.wait(ctx)
}

func (m *Manager) wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Shutdown cancels all runs and waits until they have ended or ctx is done.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.cancel()
	return m.wait(ctx)
}

func levelName(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "error"
	case l >= slog.LevelWarn:
		return "warn"
	case l >= slog.LevelInfo:
		return "info"
	}
	return "debug"
}
