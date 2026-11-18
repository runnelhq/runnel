// Package workflow runs workflows: it follows the links from the start action,
// executes each action and decides on the next one by the action's outcome.
//
// This version runs one action at a time. Parallel branches, join modes,
// expression conditions, retries and timeouts follow in later steps of the roadmap.
package workflow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/runnelhq/runnel/internal/action"
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
	// Now returns the current time; defaults to time.Now (replaceable in tests).
	Now func() time.Time
}

// Status is the final state of a run.
type Status string

// Run states.
const (
	Succeeded Status = "succeeded"
	Failed    Status = "failed"
)

// Result is the outcome of a run.
type Result struct {
	RunID    string
	Status   Status
	Err      error // reason for Failed
	Duration time.Duration
}

// EventKind classifies events of a run.
type EventKind string

// Event kinds.
const (
	RunStarted     EventKind = "run.started"
	RunFinished    EventKind = "run.finished"
	ActionStarted  EventKind = "action.started"
	ActionFinished EventKind = "action.finished"
	Log            EventKind = "log"
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
	Err        error         // ActionFinished/RunFinished: failure reason
	Status     Status        // ActionFinished/RunFinished
	Duration   time.Duration // ActionFinished/RunFinished
}

// Runner executes a prepared workflow.
type Runner struct {
	w       *model.Workflow
	opts    Options
	actions map[string]*model.Action
	out     map[string][]model.Link // enabled outgoing links per action id
	start   string
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
	r := &Runner{w: w, opts: opts, actions: map[string]*model.Action{}, out: map[string][]model.Link{}}
	var errs model.Errors
	unsupported := func(path, what string) {
		errs = append(errs, w.ErrorAt(path, "%s is not supported by this version of Runnel yet", what))
	}

	if w.Settings.Timeout != "" {
		unsupported("$.settings.timeout", "a workflow timeout")
	}
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
		if a.Timeout != "" {
			unsupported(path+".timeout", fmt.Sprintf("action %q: timeout", a.ID))
		}
		if a.Retry != nil {
			unsupported(path+".retry", fmt.Sprintf("action %q: retry", a.ID))
		}
	}

	incoming := map[string]int{}
	for i, l := range w.Links {
		if l.Disabled {
			continue
		}
		if kind, _ := l.Condition(); kind == model.OnExpression {
			unsupported(fmt.Sprintf("$.links[%d].when", i), fmt.Sprintf("link #%d: an expression condition", i+1))
		}
		r.out[l.From] = append(r.out[l.From], l)
		if !l.Loop {
			incoming[l.To]++
		}
	}
	for i, a := range w.Actions {
		if incoming[a.ID] > 1 {
			unsupported(fmt.Sprintf("$.actions[%d].id", i),
				fmt.Sprintf("action %q: more than one incoming link (joining branches)", a.ID))
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

// Run executes the workflow and returns when it has finished.
func (r *Runner) Run(ctx context.Context) Result {
	now := r.opts.Now
	started := now()
	runID := NewRunID(started)
	r.emit(Event{Time: started, Kind: RunStarted, RunID: runID, Message: r.w.Name})

	status, err := r.loop(ctx, runID)

	res := Result{RunID: runID, Status: status, Err: err, Duration: now().Sub(started)}
	r.emit(Event{Time: now(), Kind: RunFinished, RunID: runID, Status: status, Err: err, Duration: res.Duration})
	return res
}

func (r *Runner) loop(ctx context.Context, runID string) (Status, error) {
	runs := map[string]int{}
	current := r.start
	for {
		if err := ctx.Err(); err != nil {
			return Failed, err
		}
		runs[current]++
		if runs[current] > r.opts.MaxActionRuns {
			return Failed, fmt.Errorf("action %q ran more than %d times; stopping (endless loop?)", current, r.opts.MaxActionRuns)
		}

		a := r.actions[current]
		res, actErr := r.execute(ctx, runID, a)
		switch {
		case actErr == nil && res.End == action.EndSuccess:
			return Succeeded, nil
		case actErr == nil && res.End == action.EndFailure:
			return Failed, errors.New(res.Message)
		}

		ok := actErr == nil
		if !ok && a.ContinueOnError {
			r.log(runID, a, slog.LevelWarn, fmt.Sprintf("error ignored (continueOnError): %v", actErr))
			ok = true
		}

		var next []model.Link
		for _, l := range r.out[a.ID] {
			if follows(l, ok) {
				next = append(next, l)
			}
		}
		switch len(next) {
		case 0:
			if ok {
				return Succeeded, nil
			}
			return Failed, fmt.Errorf("action %q failed: %w", a.ID, actErr)
		case 1:
			current = next[0].To
		default:
			return Failed, fmt.Errorf("action %q: %d links apply (to %s and %s); parallel branches are not supported by this version of Runnel yet",
				a.ID, len(next), next[0].To, next[1].To)
		}
	}
}

// follows reports whether a link is taken after an action with the given outcome.
func follows(l model.Link, ok bool) bool {
	switch kind, _ := l.Condition(); kind {
	case model.OnSuccess:
		return ok
	case model.OnFailure:
		return !ok
	case model.Always:
		return true
	}
	return false
}

// execute runs one action: resolves variables in its configuration, creates and executes it.
func (r *Runner) execute(ctx context.Context, runID string, a *model.Action) (action.Result, error) {
	start := r.opts.Now()
	r.emit(Event{Time: start, Kind: ActionStarted, RunID: runID, ActionID: a.ID, ActionType: a.Type})

	res, err := r.executeAction(ctx, runID, a)

	status := Succeeded
	if err != nil || res.End == action.EndFailure {
		status = Failed
	}
	end := r.opts.Now()
	r.emit(Event{Time: end, Kind: ActionFinished, RunID: runID, ActionID: a.ID, ActionType: a.Type,
		Status: status, Err: err, Message: res.Message, Duration: end.Sub(start)})
	return res, err
}

func (r *Runner) executeAction(ctx context.Context, runID string, a *model.Action) (action.Result, error) {
	with, err := expand(a.With, r.opts.Scope)
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
	return act.Execute(ctx, ec)
}

func (r *Runner) log(runID string, a *model.Action, level slog.Level, msg string) {
	r.emit(Event{Time: r.opts.Now(), Kind: Log, RunID: runID, ActionID: a.ID, ActionType: a.Type, Level: level, Message: msg})
}

func (r *Runner) emit(e Event) {
	if r.opts.Observer != nil {
		r.opts.Observer(e)
	}
}

// expand resolves ${…} references in all strings of a configuration value.
func expand(v map[string]any, scope *vars.Scope) (map[string]any, error) {
	if v == nil {
		return nil, nil
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
		if scope == nil {
			return v, nil
		}
		return scope.Expand(v)
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			e, err := expandValue(val, scope)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			out[k] = e
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			e, err := expandValue(val, scope)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			out[i] = e
		}
		return out, nil
	}
	return v, nil
}
