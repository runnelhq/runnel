package runs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/runnelhq/runnel/internal/engine/pipeline"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/history"
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
	// Name is the name of the workflow or pipeline.
	Name string `json:"name"`
	// Trigger names what started the run, e.g. "cron:nightly" (S37); empty: by hand.
	Trigger string `json:"trigger,omitempty"`
	// Client is the address of the caller of an http trigger (S38a).
	Client string `json:"client,omitempty"`
	// Parent is the run this one belongs to: for an error workflow (trigger error) the
	// failed run it reports (ADR 0027).
	Parent      string            `json:"parent,omitempty"`
	Environment string            `json:"environment,omitempty"`
	Params      map[string]string `json:"params,omitempty"`
	Status      workflow.Status   `json:"status"`
	StartedAt   time.Time         `json:"startedAt"`
	FinishedAt  *time.Time        `json:"finishedAt,omitempty"`
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
	StartedAt  *time.Time      `json:"startedAt,omitempty"`
	FinishedAt *time.Time      `json:"finishedAt,omitempty"`
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
	// store and rec keep the runs in the history (ADR 0026); nil: only in memory.
	store *history.Store
	rec   *historyWriter
	// onEnd receives each run that ended and is kept (history), e.g. for the server log.
	onEnd func(Run)
	// onFailed receives each failed run with an error workflow (ADR 0027).
	onFailed func(Failure)
	// lastFailed is the last run that failed and byTrigger the status of the last ended
	// run of each trigger, since the start (GET /health, S43a).
	lastFailed *Run
	byTrigger  map[string]workflow.Status
	// steps holds the data of the steps of the last run started in the editor, the run
	// stepsRun (E23a); nil: none.
	steps    *stepRecorder
	stepsRun string
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
	// hash identifies the content of the file that runs; logSeq numbers the log lines
	// in the history.
	hash   string
	logSeq int64
	// onError is the error workflow of the run (ADR 0027); empty: none.
	onError string
}

// NewManager creates a manager. Runs are cancelled by Shutdown.
func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{ctx: ctx, cancel: cancel, runs: map[string]*entry{}, waiters: map[chan struct{}]struct{}{}}
}

// SetHistory keeps the runs in a history (ADR 0026) besides memory: runs that are no
// longer in memory are read from it. Write errors go to warn (once until a write works
// again); runs are never held up by the history. Call it before the first run and
// CloseHistory after Shutdown.
func (m *Manager) SetHistory(store *history.Store, warn func(string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store, m.rec = store, newHistoryWriter(store, warn)
}

// OnEnd calls fn for each run that has ended and is kept by its history (all, or with
// errors only the failed ones), e.g. for a line in the server log (S71). Call it before
// the first run.
func (m *Manager) OnEnd(fn func(Run)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onEnd = fn
}

// OnFailed calls fn for each run that ended with status failed and has an error workflow
// (ADR 0027), whatever its history; fn must not block. Call it before the first run.
func (m *Manager) OnFailed(fn func(Failure)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onFailed = fn
}

// CloseHistory writes what is queued for the history and stops writing to it. It does
// not close the store.
func (m *Manager) CloseHistory() {
	m.mu.Lock()
	rec := m.rec
	m.rec = nil
	m.mu.Unlock()
	if rec != nil {
		rec.close()
	}
}

// recording reports whether the runs of e go to the history. m.mu must be held.
func (m *Manager) recording(e *entry) bool {
	return m.rec != nil && e.history != HistoryNone
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

// Pending is a prepared run that has not started yet (PrepareRun, PreparePipelineRun);
// Launch starts it. In between, the caller can set up what the run's events need, e.g.
// the console log of runnel run, and choose its History.
type Pending struct {
	// History is all (default), errors or none: which ended runs are kept (S38b2, ADR 0026).
	History string
	id      string
	run     Run
	env     *model.Environment
	steps   [][2]string
	lim     limits
	hash    string
	onError string
	start   func(context.Context)
	// e is the run's entry once Launch has registered it (WaitLaunched).
	e *entry
	// recorder keeps the data of the steps (Spec.StepData); nil: none.
	recorder *stepRecorder
}

// ID returns the id of the run.
func (p *Pending) ID() string { return p.id }

// OnError returns the error workflow of the run (ADR 0027), relative to the project; empty: none.
func (p *Pending) OnError() string { return p.onError }

// Start prepares and starts a run in the background and returns its state. The spec's
// RunID is set by the manager; its Observer, if any, gets the events after the manager.
// Errors are those of Prepare.
func (m *Manager) Start(path string, spec Spec) (Run, *Prepared, error) {
	pend, p, err := m.PrepareRun(path, spec)
	if err != nil {
		return Run{}, p, err
	}
	run, err := m.Launch(pend)
	return run, p, err
}

// PrepareRun prepares a workflow run like Start, without starting it.
func (m *Manager) PrepareRun(path string, spec Spec) (*Pending, *Prepared, error) {
	id, err := m.newID()
	if err != nil {
		return nil, nil, err
	}
	spec.RunID = id
	observer := spec.Observer
	spec.Observer = func(e workflow.Event) {
		m.record(id, e)
		if observer != nil {
			observer(e)
		}
	}
	p, err := Prepare(spec)
	if err != nil {
		return nil, p, err
	}
	steps := make([][2]string, len(p.Workflow.Actions))
	for i, a := range p.Workflow.Actions {
		steps[i] = [2]string{a.ID, a.Type}
	}
	lim := limitsOf(p.Project, p.Workflow.Settings.MaxConcurrentRuns)
	if spec.Trigger == TriggerError {
		lim = limits{} // an error workflow can always report (ADR 0027)
	}
	return &Pending{History: spec.History, id: id, env: p.Env, steps: steps, hash: fileHash(spec.Data),
		run: Run{ID: id, Path: path, Kind: "workflow", Name: p.Workflow.Name, Params: spec.Params, Trigger: spec.Trigger,
			Client: spec.Client, Parent: spec.Parent},
		lim:      lim,
		onError:  onErrorOf(spec.Trigger, spec.NoOnError, p.Project, p.Workflow.Settings.OnError, path),
		start:    func(ctx context.Context) { p.Runner.Run(ctx) },
		recorder: p.steps}, p, nil
}

// StartPipeline prepares and starts a pipeline run in the background, like Start (S25a3).
// Its transforms are reported as actions, with their counts when they have finished.
func (m *Manager) StartPipeline(path string, spec PipelineSpec) (Run, *PreparedPipeline, error) {
	pend, p, err := m.PreparePipelineRun(path, spec)
	if err != nil {
		return Run{}, p, err
	}
	run, err := m.Launch(pend)
	return run, p, err
}

// PreparePipelineRun prepares a pipeline run like StartPipeline, without starting it.
func (m *Manager) PreparePipelineRun(path string, spec PipelineSpec) (*Pending, *PreparedPipeline, error) {
	id, err := m.newID()
	if err != nil {
		return nil, nil, err
	}
	spec.RunID = id
	observer := spec.Observer
	spec.Observer = func(e pipeline.Event) {
		m.recordPipeline(id, e)
		if observer != nil {
			observer(e)
		}
	}
	p, err := PreparePipeline(spec)
	if err != nil {
		return nil, p, err
	}
	steps := make([][2]string, len(p.Pipeline.Transforms))
	for i, t := range p.Pipeline.Transforms {
		steps[i] = [2]string{t.ID, t.Type}
	}
	return &Pending{History: spec.History, id: id, env: p.Env, steps: steps, hash: fileHash(spec.Data),
		run: Run{ID: id, Path: path, Kind: "pipeline", Name: p.Pipeline.Name, Params: spec.Params, Trigger: spec.Trigger, Client: spec.Client,
			LinkRows: make([]int64, len(p.Pipeline.Links))},
		lim:     limitsOf(p.Project, 0),
		onError: onErrorOf(spec.Trigger, spec.NoOnError, p.Project, p.Pipeline.Settings.OnError, path),
		start:   func(ctx context.Context) { p.Runner.Run(ctx) }}, p, nil
}

// Launch starts a prepared run in the background, unless a limit of runs at the same
// time is reached or the manager is shutting down.
func (m *Manager) Launch(p *Pending) (Run, error) {
	return m.add(p)
}

// add registers a prepared run with its steps (id and type of the actions or transforms)
// and runs it in the background, unless a limit of runs at the same time is reached.
func (m *Manager) add(p *Pending) (Run, error) {
	ctx, cancel := context.WithCancel(m.ctx)
	id, run := p.id, p.run
	run.Status, run.StartedAt = StatusRunning, time.Now()
	if p.env != nil {
		run.Environment = p.env.Name
	}
	e := &entry{run: run, byID: map[string]*ActionState{}, cancel: cancel, history: p.History, hash: p.hash, onError: p.onError, done: make(chan struct{})}
	for _, st := range p.steps {
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
	if err := m.checkLimits(run.Path, p.lim); err != nil {
		m.mu.Unlock()
		cancel()
		return Run{}, err
	}
	m.runs[id] = e
	m.order = append(m.order, id)
	p.e = e
	if p.recorder != nil { // the data of the previous run of the editor is dropped
		m.steps, m.stepsRun = p.recorder, id
	}
	m.prune()
	if m.recording(e) {
		m.rec.send(recOp{kind: recRun, run: snapshot(e), hash: e.hash})
	}
	out := e.run
	out.LinkRows = slices.Clone(out.LinkRows) // the events change the entry's slice
	m.mu.Unlock()

	m.wg.Go(func() {
		defer cancel()
		p.start(ctx)
		m.mu.Lock()
		kept := m.ended(e)
		onEnd, run := m.onEnd, snapshot(e)
		onFailed, failed := m.onFailed, e.run.Status == workflow.Failed && e.onError != ""
		var f Failure
		if onFailed != nil && failed {
			f = FailureOf(detailOf(e), e.onError)
		}
		m.mu.Unlock()
		if onEnd != nil && kept {
			onEnd(run)
		}
		if onFailed != nil && failed {
			onFailed(f)
		}
	})
	return out, nil
}

// checkLimits reports a LimitError if starting another run of path would exceed a
// limit. m.mu must be held.
func (m *Manager) checkLimits(path string, lim limits) error {
	all, same := 0, 0
	for _, e := range m.runs {
		if e.run.Status != StatusRunning || e.run.Trigger == TriggerError { // error workflows do not count (ADR 0027)
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

// ended applies the history of a run that has ended and reports whether it is kept.
// m.mu must be held.
func (m *Manager) ended(e *entry) bool {
	close(e.done)
	if e.run.Trigger != "" {
		if m.byTrigger == nil {
			m.byTrigger = map[string]workflow.Status{}
		}
		m.byTrigger[e.run.Trigger] = e.run.Status
	}
	if e.run.Status == workflow.Failed {
		r := e.run
		r.Params, r.Outputs, r.LinkRows = nil, nil, nil
		m.lastFailed = &r
	}
	drop := e.history == HistoryNone || (e.history == HistoryErrors && e.run.Status == workflow.Succeeded)
	if m.recording(e) {
		m.rec.send(recOp{kind: recEnd, run: snapshot(e), hash: e.hash, nodes: copyActions(e), del: drop})
	}
	if !drop {
		return true
	}
	if i := slices.Index(m.order, e.run.ID); i >= 0 {
		m.remove(i)
	}
	return false
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
		e.run.StartedAt = we.Time
		ev.Message = e.run.Name
	case workflow.ActionStarted:
		if a != nil {
			t := we.Time
			a.Status, a.StartedAt, a.FinishedAt, a.DurationMs, a.Error = StatusRunning, &t, nil, 0, ""
			a.Runs++
		}
	case workflow.ActionWaiting:
		if a != nil {
			a.Status = workflow.Waiting
		}
	case workflow.ActionFinished, workflow.ActionSkipped:
		if a != nil {
			t := we.Time
			a.FinishedAt = &t
			a.Status, a.DurationMs, a.Error = we.Status, ev.DurationMs, ev.Error
			if we.Kind == workflow.ActionSkipped {
				a.Status = workflow.Skipped
				ev.Status = workflow.Skipped
			}
			a.Counts = counts
			if m.recording(e) {
				m.rec.send(recOp{kind: recNode, run: Run{ID: id}, index: slices.Index(e.actions, a), node: *a})
			}
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
		if m.recording(e) {
			e.logSeq++
			m.rec.send(recOp{kind: recLog, run: Run{ID: id},
				line: history.LogLine{Seq: e.logSeq, Time: we.Time, Level: ev.Level, ActionID: we.ActionID, Message: we.Message}})
		}
	case workflow.RunFinished:
		t := we.Time
		e.run.Status, e.run.FinishedAt, e.run.DurationMs, e.run.Error = we.Status, &t, ev.DurationMs, ev.Error
		e.run.Outputs, ev.Outputs = we.Outputs, we.Outputs
		ev.Message = e.run.Name
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

// Runs returns the runs in memory and, with a history, the newest ones from it; newest
// first, at most MaxRuns+MaxHTTPRuns. A history that cannot be read is reported as error
// together with the runs in memory.
func (m *Manager) Runs(ctx context.Context) ([]Run, error) {
	return m.Find(ctx, history.Filter{})
}

// MaxList is the most runs Find returns; DefaultList those without a limit.
const (
	MaxList     = 1000
	DefaultList = MaxRuns + MaxHTTPRuns
)

// Find returns the runs that match f (S26b), from memory and the history, newest first,
// at most f.Limit (DefaultList if 0, at most MaxList).
func (m *Manager) Find(ctx context.Context, f history.Filter) ([]Run, error) {
	if f.Limit <= 0 {
		f.Limit = DefaultList
	}
	f.Limit = min(f.Limit, MaxList)
	m.mu.Lock()
	out := make([]Run, 0, len(m.order))
	// The state in memory is the current one; the history may lag behind (it is written
	// in the background), e.g. still "running" for a run that has just ended. So a run in
	// memory is never taken from the history, whether or not it matches f.
	inMemory := make(map[string]bool, len(m.order))
	for _, id := range slices.Backward(m.order) {
		r := m.runs[id].run
		inMemory[id] = true
		if matches(r, f) {
			r.LinkRows = slices.Clone(r.LinkRows)
			out = append(out, r)
		}
	}
	store := m.store
	m.mu.Unlock()
	if store == nil {
		if len(out) > f.Limit {
			out = out[:f.Limit]
		}
		return out, nil
	}
	stored, err := store.Find(ctx, f)
	if err != nil {
		return out, err
	}
	runs := make([]Run, 0, len(stored))
	for _, h := range stored {
		if inMemory[h.ID] {
			continue
		}
		r, err := fromHistory(h)
		if err != nil {
			return out, err
		}
		runs = append(runs, r)
	}
	return mergeRuns(out, runs, f.Limit), nil
}

// matches reports whether a run in memory matches a filter.
func matches(r Run, f history.Filter) bool {
	return (f.Status == "" || string(r.Status) == f.Status) && (f.Path == "" || r.Path == f.Path) &&
		(f.Trigger == "" || r.Trigger == f.Trigger || f.Trigger == history.TriggerEditor && r.Trigger == "") &&
		(f.Parent == "" || r.Parent == f.Parent) && (f.Before == "" || r.ID < f.Before)
}

// Get returns a run with its actions and log, from memory or else from the history.
func (m *Manager) Get(ctx context.Context, id string) (Detail, bool, error) {
	m.mu.Lock()
	if e := m.runs[id]; e != nil {
		defer m.mu.Unlock()
		return detailOf(e), true, nil
	}
	store := m.store
	m.mu.Unlock()
	if store == nil {
		return Detail{}, false, nil
	}
	h, ok, err := store.Get(ctx, id)
	if !ok || err != nil {
		return Detail{}, false, err
	}
	d, err := detailFromHistory(h)
	return d, err == nil, err
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
// ends, if it was running when Wait was called; a caller that started the run uses
// WaitLaunched, which cannot miss it.
func (m *Manager) Wait(ctx context.Context, id string) (Detail, bool) {
	m.mu.Lock()
	e := m.runs[id]
	m.mu.Unlock()
	if e == nil {
		return Detail{}, false
	}
	return m.waitEntry(ctx, e), true
}

// WaitLaunched waits like Wait for the run that Launch started from p, and returns its
// state even if the run has already ended and its history (errors, none) dropped it from
// memory: unlike Wait by id, it cannot miss a run that ends before it is called. False
// if p was not launched.
func (m *Manager) WaitLaunched(ctx context.Context, p *Pending) (Detail, bool) {
	m.mu.Lock()
	e := p.e
	m.mu.Unlock()
	if e == nil {
		return Detail{}, false
	}
	return m.waitEntry(ctx, e), true
}

// waitEntry waits until the run of e has ended or ctx is done and returns its state.
func (m *Manager) waitEntry(ctx context.Context, e *entry) Detail {
	select {
	case <-e.done:
	case <-ctx.Done():
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return detailOf(e)
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

// Health summarizes the runs for GET /health (S43a), from memory only.
type Health struct {
	// Running is the number of running runs.
	Running int
	// LastFailed is the last run that failed since the start; nil: none.
	LastFailed *Run
	// Triggers is the status of the last ended run of each trigger ("cron:nightly").
	Triggers map[string]workflow.Status
	// HistoryError is the error of the last write to the run history; empty if it
	// worked or there is no history.
	HistoryError string
}

// Health returns the summary of the runs.
func (m *Manager) Health() Health {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := Health{LastFailed: m.lastFailed, Triggers: maps.Clone(m.byTrigger)}
	for _, e := range m.runs {
		if e.run.Status == StatusRunning {
			h.Running++
		}
	}
	if m.rec != nil {
		h.HistoryError = m.rec.lastError()
	}
	return h
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
	m.mu.Lock()
	m.steps = nil
	m.mu.Unlock()
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
