package runs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/model"
)

// Error workflow (ADR 0027): a workflow of the project that runs after a failed run with
// the FAILED_… parameters it declares.

// TriggerError is the trigger of the runs of error workflows. They start no error
// workflow themselves.
const TriggerError = "error"

// Parameters given to an error workflow, if it declares them.
const (
	ParamFailedRun      = "FAILED_RUN"
	ParamFailedPath     = "FAILED_PATH"
	ParamFailedName     = "FAILED_NAME"
	ParamFailedAction   = "FAILED_ACTION"
	ParamFailedError    = "FAILED_ERROR"
	ParamFailedTrigger  = "FAILED_TRIGGER"
	ParamFailedStarted  = "FAILED_STARTED"
	ParamFailedFinished = "FAILED_FINISHED"
)

// FailedParams lists the parameters an error workflow can declare.
var FailedParams = []string{ParamFailedRun, ParamFailedPath, ParamFailedName, ParamFailedAction,
	ParamFailedError, ParamFailedTrigger, ParamFailedStarted, ParamFailedFinished}

// MaxOnErrorQueue is the number of failures that wait for their error workflow; more are dropped.
const MaxOnErrorQueue = 20

// Failure is a failed run to report with its error workflow.
type Failure struct {
	// Run is the failed run; its ID is empty when it failed before it started.
	Run Run
	// Action is the id of the first failed action or transform; empty if none.
	Action string
	// Target is the error workflow, relative to the project with "/".
	Target string
}

// onErrorOf returns the error workflow of a run (ADR 0027): none for runs of the editor
// (no trigger), of error workflows and with noOnError. A workflow that names itself is
// ignored (validate warns), so that the setting of the project applies.
func onErrorOf(trigger string, noOnError bool, p *model.Project, own, path string) string {
	if trigger == "" || trigger == TriggerError || noOnError {
		return ""
	}
	if own != "" && own != model.OnErrorNone && model.OnErrorTarget(nil, own) == filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) {
		own = ""
	}
	return model.OnErrorTarget(p, own)
}

// FailureOf describes a failed run for its error workflow target.
func FailureOf(d Detail, target string) Failure {
	f := Failure{Run: d.Run, Target: target}
	f.Run.Params, f.Run.Outputs, f.Run.LinkRows = nil, nil, nil // not passed on (ADR 0027, Sicherheit)
	var first *time.Time
	for _, a := range d.Actions {
		if a.Status != workflow.Failed {
			continue
		}
		if f.Action == "" || (a.FinishedAt != nil && (first == nil || a.FinishedAt.Before(*first))) {
			f.Action, first = a.ID, a.FinishedAt
		}
	}
	return f
}

// StartFailure describes a run of a trigger or runnel run that could not be started (err)
// for its error workflow (ADR 0027): path is the file relative to the project, data its
// content. It reports false when no error workflow applies, also for limits of runs at
// the same time and a shutdown.
func StartFailure(projectDir, path string, data []byte, trigger, env string, noOnError bool, err error) (Failure, bool) {
	if err == nil || errors.Is(err, ErrShutdown) || errors.As(err, new(*LimitError)) {
		return Failure{}, false
	}
	run := Run{Path: path, Trigger: trigger, Environment: env, Status: workflow.Failed, Error: err.Error(), StartedAt: time.Now()}
	finished := run.StartedAt
	run.FinishedAt = &finished
	var own string
	if strings.HasSuffix(path, ".pipeline.yaml") {
		run.Kind = "pipeline"
		if p, _ := model.ReadPipeline(path, data); p != nil {
			run.Name, own = p.Name, p.Settings.OnError
		}
	} else {
		run.Kind = "workflow"
		if w, _ := model.ReadWorkflow(path, data); w != nil {
			run.Name, own = w.Name, w.Settings.OnError
		}
	}
	var project *model.Project
	if projectDir != "" {
		project, _ = model.LoadProject(projectDir) // a project with errors still names its error workflow
	}
	target := onErrorOf(trigger, noOnError, project, own, path)
	return Failure{Run: run, Target: target}, target != ""
}

// Params returns the values of the FAILED_… parameters the error workflow declares.
func (f Failure) Params(declared []model.Parameter) map[string]string {
	values := map[string]string{
		ParamFailedRun: f.Run.ID, ParamFailedPath: f.Run.Path, ParamFailedName: f.Run.Name, ParamFailedAction: f.Action,
		ParamFailedError: f.Run.Error, ParamFailedTrigger: f.Run.Trigger, ParamFailedStarted: rfc3339(&f.Run.StartedAt),
		ParamFailedFinished: rfc3339(f.Run.FinishedAt),
	}
	out := map[string]string{}
	for _, p := range declared {
		if v, ok := values[p.Name]; ok {
			out[p.Name] = v
		}
	}
	return out
}

func rfc3339(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// ErrorSpec returns the spec of the run of the error workflow of f (file on disk, data
// its content): the declared FAILED_… parameters, trigger error, the failed run as
// parent and its environment. The caller sets the registries, the observer and the history.
func ErrorSpec(f Failure, projectDir, file string, data []byte) Spec {
	spec := Spec{Name: f.Target, File: file, Data: data,
		ProjectDir: projectDir, Env: f.Run.Environment, Trigger: TriggerError, Parent: f.Run.ID, Params: map[string]string{}}
	if w, err := model.ParseWorkflow(f.Target, data); err == nil {
		spec.Params = f.Params(w.Parameters)
	}
	return spec
}

// Describe names the failed run for messages, e.g. "run 01J… of workflows/a.workflow.yaml".
func (f Failure) Describe() string {
	if f.Run.ID == "" {
		return f.Run.Path + " (not started)"
	}
	return "run " + f.Run.ID + " of " + f.Run.Path
}

// ErrorQueue runs the error workflows of a server one after the other (ADR 0027): at
// most one at a time, at most MaxOnErrorQueue failures wait; more are dropped with a
// warning.
type ErrorQueue struct {
	run  func(Failure)
	warn func(string)

	mu      sync.Mutex
	waiting []Failure
	busy    bool
	closed  bool
}

// NewErrorQueue returns a queue that calls run for each failure, one at a time; run
// starts the error workflow and returns when it has ended. warn receives dropped failures.
func NewErrorQueue(run func(Failure), warn func(string)) *ErrorQueue {
	if warn == nil {
		warn = func(string) {}
	}
	return &ErrorQueue{run: run, warn: warn}
}

// Add queues a failure without waiting.
func (q *ErrorQueue) Add(f Failure) {
	q.mu.Lock()
	defer q.mu.Unlock()
	switch {
	case q.closed:
		q.warn(fmt.Sprintf("on error: shutting down, %s not started for %s", f.Target, f.Describe()))
	case !q.busy:
		q.busy = true
		go q.loop(f)
	case len(q.waiting) >= MaxOnErrorQueue:
		q.warn(fmt.Sprintf("on-error queue full: %s not started for %s (%d failures waiting)", f.Target, f.Describe(), len(q.waiting)))
	default:
		q.waiting = append(q.waiting, f)
	}
}

// Len returns the number of failures waiting for their error workflow.
func (q *ErrorQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.waiting)
}

func (q *ErrorQueue) loop(f Failure) {
	for {
		q.run(f)
		q.mu.Lock()
		if q.closed || len(q.waiting) == 0 {
			q.busy = false
			q.mu.Unlock()
			return
		}
		f = q.waiting[0]
		q.waiting = slices.Delete(q.waiting, 0, 1)
		q.mu.Unlock()
	}
}

// Close drops the waiting failures, each with a warning, and accepts no new ones. The
// running error workflow is not stopped; it is a run like others.
func (q *ErrorQueue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	for _, f := range q.waiting {
		q.warn(fmt.Sprintf("on error: shutting down, %s not started for %s", f.Target, f.Describe()))
	}
	q.waiting = nil
}

// CheckOnError checks settings.onError of project.yaml, a workflow or a pipeline (ADR
// 0027) against the project: the error workflow exists, is a workflow, has no required
// parameters besides FAILED_… and declares those as string. self is the file of the
// setting ("" for project.yaml); a workflow that names itself gets a warning. errAt and
// warnAt locate the problems at "$.settings.onError".
func CheckOnError(errAt, warnAt ErrorAt, value, projectDir, self string) model.Errors {
	const at = "$.settings.onError"
	if value == "" || value == model.OnErrorNone || model.CheckOnErrorPath(value) != "" {
		return nil // the syntax is checked with the file
	}
	file, name := Resolve(projectDir, self, value)
	if self != "" && absolute(file) == absolute(self) {
		return model.Errors{warnAt(at, "onError names this workflow itself; it is ignored here (the setting of %s applies)", model.ProjectFile)}
	}
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return model.Errors{errAt(at, "error workflow %s not found", name)}
	} else if err != nil {
		return model.Errors{errAt(at, "cannot read error workflow %s: %v", name, err)}
	}
	w, err := model.ParseWorkflow(name, data)
	if err != nil {
		return model.Errors{errAt(at, "error workflow %s has errors; check it with \"runnel validate\"", name)}
	}
	var errs model.Errors
	for _, p := range w.Parameters {
		switch {
		case slices.Contains(FailedParams, p.Name):
			if p.Type.OrString() != model.TypeString {
				errs = append(errs, errAt(at, "error workflow %s: parameter %s must be of type string, not %s", name, p.Name, p.Type))
			}
		case p.Required && p.Default == "":
			errs = append(errs, errAt(at, "error workflow %s: required parameter %s is not given; an error workflow only gets %s",
				name, p.Name, strings.Join(FailedParams, ", ")))
		}
	}
	return errs
}
