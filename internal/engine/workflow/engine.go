// Package workflow runs workflows: it follows the links from the start action,
// executes each action and decides on the next one by the action's outcome.
//
// Actions whose incoming links fire run in parallel; where branches meet, the join
// mode of the action decides when it starts (ADR 0003). Expression conditions,
// retries and timeouts follow in later steps of the roadmap.
package workflow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/vars"
)

// DefaultMaxActionRuns limits how often a single action may run in one workflow run,
// so that a loop without an exit condition ends.
const DefaultMaxActionRuns = 10000

// Options configure a run.
type Options struct {
	Registry *action.Registry
	// Scope resolves ${…} references in the action configuration.
	Scope *vars.Scope
	// Observer receives the events of the run; may be nil.
	Observer func(Event)
	// MaxActionRuns overrides DefaultMaxActionRuns if > 0.
	MaxActionRuns int
	// DefaultJoin applies to actions without "join"; empty means all (ADR 0003).
	DefaultJoin model.JoinMode
	// Now returns the current time; defaults to time.Now (replaceable in tests).
	Now func() time.Time
	// RunID is the id of the run; if empty, Run creates one (NewRunID). The server
	// sets it to return the id before the run starts.
	RunID string
	// Environment is the name of the environment of the run (run.environment in expressions).
	Environment string
}

// Status is the final state of a run.
type Status string

// Run and action states.
const (
	Succeeded Status = "succeeded"
	Failed    Status = "failed"
	// Skipped: the action was not run because none of its incoming links can fire (ADR 0003).
	Skipped Status = "skipped"
	// Cancelled: the run was stopped from outside (e.g. Ctrl+C).
	Cancelled Status = "cancelled"
	// Waiting: the action waits for further incoming links (join all); not a final state.
	Waiting Status = "waiting"
)

// Result is the outcome of a run.
type Result struct {
	RunID    string
	Status   Status
	Err      error // reason for Failed
	Duration time.Duration
	// Outputs holds the workflow outputs (typed values by name) if the run succeeded.
	Outputs map[string]any
}

// EventKind classifies events of a run.
type EventKind string

// Event kinds.
const (
	RunStarted     EventKind = "run.started"
	RunFinished    EventKind = "run.finished"
	ActionStarted  EventKind = "action.started"
	ActionFinished EventKind = "action.finished"
	ActionSkipped  EventKind = "action.skipped"
	// ActionWaiting: an incoming link of an action with join all has fired, but others are
	// still pending; Message says how many (sent again for every further link that fires).
	ActionWaiting EventKind = "action.waiting"
	Log           EventKind = "log"
)

// Event is something that happened during a run.
type Event struct {
	Time       time.Time
	Kind       EventKind
	RunID      string
	ActionID   string // empty for run events
	ActionType string
	Level      slog.Level // for Log
	Message    string
	Err        error          // ActionFinished/RunFinished: failure reason
	Status     Status         // ActionFinished/RunFinished
	Duration   time.Duration  // ActionFinished/RunFinished
	Outputs    map[string]any // RunFinished: workflow outputs of a successful run
}

// Runner executes a prepared workflow.
type Runner struct {
	w       *model.Workflow
	opts    Options
	actions map[string]*model.Action
	out     map[string][]int // enabled outgoing links (indexes) per action id
	in      map[string][]int // enabled incoming non-loop links (indexes) per action id
	start   string
	emitMu  sync.Mutex
	// templates caches compiled expressions by text (ADR 0006: compiled once).
	templates sync.Map
}

// Prepare checks that the workflow can be run by this engine: all action types exist,
// their configuration is well-formed and only supported features are used.
// The workflow must have been loaded with model.LoadWorkflow and validated with
// Workflow.Validate without errors. All problems are returned as model.Errors.
func Prepare(w *model.Workflow, opts Options) (*Runner, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.MaxActionRuns <= 0 {
		opts.MaxActionRuns = DefaultMaxActionRuns
	}
	r := &Runner{w: w, opts: opts, actions: map[string]*model.Action{}, out: map[string][]int{}, in: map[string][]int{}}
	var errs model.Errors
	for i := range w.Actions {
		a := &w.Actions[i]
		path := fmt.Sprintf("$.actions[%d]", i)
		r.actions[a.ID] = a
		if a.Type == model.ActionTypeStart {
			r.start = a.ID
		}
		def, ok := opts.Registry.Lookup(a.Type)
		if !ok {
			errs = append(errs, w.ErrorAt(path+".type", "action %q: unknown action type %q (available: %v)",
				a.ID, a.Type, opts.Registry.Types()))
		} else if _, err := def.New(a.With); err != nil {
			errs = append(errs, w.ErrorAt(path+".with", "action %q (%s): %v", a.ID, a.Type, err))
		}
	}

	for i, l := range w.Links {
		if l.Disabled {
			continue
		}
		if kind, _ := l.Condition(); kind == model.OnExpression {
			if err := expr.Check(l.When, expr.WorkflowRoots); err != nil {
				errs = append(errs, w.ErrorAt(fmt.Sprintf("$.links[%d].when", i), "link #%d: %v", i+1, err))
			}
		}
		r.out[l.From] = append(r.out[l.From], i)
		if !l.Loop {
			r.in[l.To] = append(r.in[l.To], i)
		}
	}
	if r.start == "" {
		errs = append(errs, w.ErrorAt("$.actions", "workflow has no start action"))
	}
	if err := errs.Err(); err != nil {
		return nil, err
	}
	return r, nil
}

// Check returns the problems Prepare finds – unknown action types, malformed
// configurations, unsupported features – without preparing a run. It is used by
// "runnel validate" and the editor's validation, so that they report what a run
// would reject. The workflow must have been validated without errors.
func Check(w *model.Workflow, opts Options) model.Errors {
	_, err := Prepare(w, opts)
	if err == nil {
		return nil
	}
	var diags model.Errors
	if errors.As(err, &diags) {
		return diags
	}
	return model.Errors{w.ErrorAt("$", "%v", err)}
}

// Run executes the workflow and returns when it has finished.
func (r *Runner) Run(ctx context.Context) Result {
	now := r.opts.Now
	started := now()
	runID := r.opts.RunID
	if runID == "" {
		runID = NewRunID(started)
	}
	r.emit(Event{Time: started, Kind: RunStarted, RunID: runID, Message: r.w.Name})

	runCtx, cancel := ctx, context.CancelFunc(func() {})
	timeout, _ := r.w.Settings.Timeout.Value()
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	var status Status
	rc, err := r.newRunContext(runID, started)
	if err != nil {
		status = Failed
	} else {
		status, err = newScheduler(runCtx, r, runID, rc).run()
	}
	cancel()
	// A cancellation or timeout that only happened after the run had succeeded does not count.
	switch {
	case status == Succeeded:
	case ctx.Err() != nil:
		status, err = Cancelled, fmt.Errorf("run cancelled: %w", context.Cause(ctx))
	case timeout > 0 && errors.Is(runCtx.Err(), context.DeadlineExceeded):
		status, err = Failed, fmt.Errorf("workflow timed out after %s", timeout)
	}

	var outputs map[string]any
	if status == Succeeded {
		if outputs, err = r.outputs(rc); err != nil {
			status = Failed
		}
	}

	res := Result{RunID: runID, Status: status, Err: err, Duration: now().Sub(started), Outputs: outputs}
	r.emit(Event{Time: now(), Kind: RunFinished, RunID: runID, Status: status, Err: err,
		Duration: res.Duration, Outputs: outputs})
	return res
}

// outputs evaluates the workflow outputs after a successful run, with the results of
// all actions (results) available to expressions.
func (r *Runner) outputs(rc *runContext) (map[string]any, error) {
	if len(r.w.Outputs) == 0 {
		return nil, nil
	}
	values := rc.values(nil)
	out := make(map[string]any, len(r.w.Outputs))
	for _, o := range r.w.Outputs {
		v, err := r.evalText(o.Value, rc.scope, values)
		if err != nil {
			return nil, fmt.Errorf("output %q: %w", o.Name, err)
		}
		text, ok := v.(string)
		if !ok {
			text = expr.Format(v)
		}
		if out[o.Name], err = o.Type.Parse(text); err != nil {
			return nil, fmt.Errorf("output %q: %w", o.Name, err)
		}
	}
	return out, nil
}

// executeAction runs an action including retries and per-attempt timeouts.
// values is the context of its expressions (ADR 0006).
func (r *Runner) executeAction(ctx context.Context, runID string, a *model.Action, scope *vars.Scope, values map[string]any) (action.Result, error) {
	attempts := 1
	var retry model.Retry
	if a.Retry != nil {
		retry = *a.Retry
		attempts = max(retry.Attempts, 1)
	}
	for attempt := 1; ; attempt++ {
		res, err := r.attempt(ctx, runID, a, scope, values)
		if err == nil || attempt >= attempts || ctx.Err() != nil {
			return res, err
		}
		wait := retry.Wait(attempt + 1)
		r.log(runID, a, slog.LevelWarn, fmt.Sprintf("attempt %d of %d failed: %v; retrying in %s", attempt, attempts, err, wait))
		if !sleep(ctx, wait) {
			return res, err
		}
	}
}

// attempt runs an action once: resolves variables, creates and executes it within its timeout.
func (r *Runner) attempt(ctx context.Context, runID string, a *model.Action, scope *vars.Scope, values map[string]any) (action.Result, error) {
	timeout, _ := a.Timeout.Value()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeoutCause(ctx, timeout, errActionTimeout)
		defer cancel()
	}

	with, err := r.evalWith(a, scope, values)
	if err != nil {
		return action.Result{}, err
	}
	def, _ := r.opts.Registry.Lookup(a.Type)
	act, err := def.New(with)
	if err != nil {
		return action.Result{}, err
	}
	ec := &action.ExecContext{
		RunID:    runID,
		ActionID: a.ID,
		Log:      func(level slog.Level, msg string) { r.log(runID, a, level, msg) },
	}
	res, err := act.Execute(ctx, ec)
	if err != nil && errors.Is(context.Cause(ctx), errActionTimeout) {
		err = fmt.Errorf("timed out after %s", timeout)
	}
	return res, err
}

// errActionTimeout marks the cancellation of an action by its own timeout.
var errActionTimeout = errors.New("action timeout")

// sleep waits for d or until ctx is done; it reports whether the full time passed.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (r *Runner) log(runID string, a *model.Action, level slog.Level, msg string) {
	r.emit(Event{Time: r.opts.Now(), Kind: Log, RunID: runID, ActionID: a.ID, ActionType: a.Type, Level: level, Message: msg})
}

// emit passes an event to the observer. Events come from the scheduler and from
// running actions (log messages); the observer is called by one goroutine at a time.
func (r *Runner) emit(e Event) {
	if r.opts.Observer != nil {
		r.emitMu.Lock()
		defer r.emitMu.Unlock()
		r.opts.Observer(e)
	}
}
