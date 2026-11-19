package runs

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/runnelhq/runnel/internal/engine/workflow"
)

// Limits of the in-memory history. The SQLite history (roadmap S26) replaces it.
const (
	// MaxRuns is the number of runs kept; the oldest finished runs are dropped first.
	MaxRuns = 100
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
	ID          string            `json:"id"`
	Path        string            `json:"path"`
	Workflow    string            `json:"workflow"`
	Environment string            `json:"environment,omitempty"`
	Params      map[string]string `json:"params,omitempty"`
	Status      workflow.Status   `json:"status"`
	Started     time.Time         `json:"started"`
	Finished    *time.Time        `json:"finished,omitempty"`
	DurationMs  int64             `json:"durationMs"`
	Error       string            `json:"error,omitempty"`
	Outputs     map[string]any    `json:"outputs,omitempty"`
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
}

type entry struct {
	run          Run
	actions      []*ActionState
	byID         map[string]*ActionState
	log          []LogLine
	logTruncated bool
	cancel       context.CancelFunc
}

// NewManager creates a manager. Runs are cancelled by Shutdown.
func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{ctx: ctx, cancel: cancel, runs: map[string]*entry{}, waiters: map[chan struct{}]struct{}{}}
}

// ErrShutdown is returned by Start after Shutdown.
var ErrShutdown = errors.New("the server is shutting down")

// Start prepares and starts a run in the background and returns its state. The
// spec's Observer and RunID are set by the manager. Errors are those of Prepare.
func (m *Manager) Start(path string, spec Spec) (Run, *Prepared, error) {
	if m.ctx.Err() != nil {
		return Run{}, nil, ErrShutdown
	}
	id := workflow.NewRunID(time.Now())
	spec.RunID = id
	spec.Observer = func(e workflow.Event) { m.record(id, e) }
	p, err := Prepare(spec)
	if err != nil {
		return Run{}, p, err
	}

	ctx, cancel := context.WithCancel(m.ctx)
	e := &entry{
		run: Run{ID: id, Path: path, Workflow: p.Workflow.Name, Params: spec.Params,
			Status: StatusRunning, Started: time.Now()},
		byID:   map[string]*ActionState{},
		cancel: cancel,
	}
	if p.Env != nil {
		e.run.Environment = p.Env.Name
	}
	for _, a := range p.Workflow.Actions {
		s := &ActionState{ID: a.ID, Type: a.Type, Status: StatusPending}
		e.actions = append(e.actions, s)
		e.byID[a.ID] = s
	}
	m.mu.Lock()
	m.runs[id] = e
	m.order = append(m.order, id)
	m.prune()
	run := e.run
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer cancel()
		p.Runner.Run(ctx)
	}()
	return run, p, nil
}

// prune drops the oldest finished runs beyond MaxRuns. m.mu must be held.
func (m *Manager) prune() {
	for i := 0; len(m.order) > MaxRuns && i < len(m.order); {
		id := m.order[i]
		if m.runs[id].run.Status == StatusRunning {
			i++
			continue
		}
		delete(m.runs, id)
		m.order = slices.Delete(m.order, i, i+1)
	}
}

// record applies an event of the engine to the run state and publishes it.
func (m *Manager) record(id string, we workflow.Event) {
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
	m.publish(ev)
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
	for i := len(m.order) - 1; i >= 0; i-- {
		out = append(out, m.runs[m.order[i]].run)
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
	d := Detail{Run: e.run, Log: slices.Clone(e.log), LogTruncated: e.logTruncated}
	for _, a := range e.actions {
		c := *a
		d.Actions = append(d.Actions, &c)
	}
	if d.Log == nil {
		d.Log = []LogLine{}
	}
	return d, true
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

// Shutdown cancels all runs and waits until they have ended or ctx is done.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.cancel()
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
