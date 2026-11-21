// Package runs prepares workflow runs and keeps track of the runs of a server.
//
// Prepare is shared by "runnel run" and the server, so that both accept and reject
// the same workflows, environments and parameters.
package runs

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/transform"
)

// Spec describes a run to prepare.
type Spec struct {
	// Name identifies the workflow file in messages, e.g. "workflows/import.workflow.yaml".
	Name string
	// File is the path of the workflow file on disk, if Name is not (e.g. relative to
	// the project); called workflows are found relative to it without a project.
	File string
	// Data is the content of the workflow file.
	Data []byte
	// ProjectDir is the directory of the project (with project.yaml); "" if the
	// workflow has no project.
	ProjectDir string
	// Env names the environment; "" selects the project's default environment.
	Env string
	// Params are the parameter values given at start.
	Params map[string]string

	Registry *action.Registry
	// Transforms are the transform types of pipelines called with core.run-pipeline;
	// nil: calling pipelines is not available.
	Transforms *transform.Registry
	Observer   func(workflow.Event)
	RunID      string
	// Trigger names what started the run, e.g. "cron:nightly"; empty: started by hand.
	Trigger string
	// Client is the address of the caller of an http trigger.
	Client string
	// Request is the request of the caller of an http trigger (request in
	// expressions) and Respond answers it (http.respond); both are passed on to
	// called workflows (S38b1).
	Request map[string]any
	Respond action.Responder

	// chain lists the workflows that called this one (core.run-workflow).
	chain []caller
	// strict rejects parameters the workflow does not declare (called workflows).
	strict bool
}

// file returns the path of the workflow file on disk.
func (spec Spec) file() string {
	if spec.File != "" {
		return spec.File
	}
	return spec.Name
}

// Prepared is the result of Prepare. It is also returned when Prepare fails, holding
// what was found up to the failure.
type Prepared struct {
	Runner   *workflow.Runner
	Workflow *model.Workflow
	Project  *model.Project
	Env      *model.Environment
	// Warnings found while checking the files; they do not prevent the run.
	Warnings model.Errors
	// Undeclared lists parameters given at start but not declared by the workflow;
	// they are still available as variables.
	Undeclared []string
}

// Prepare loads, checks and prepares a run. Problems in the files are returned as
// model.Errors (including the warnings found at that stage); problems with the
// environment or parameters as plain errors.
func Prepare(spec Spec) (*Prepared, error) {
	p := &Prepared{}
	w, err := model.ParseWorkflow(spec.Name, spec.Data)
	if err != nil {
		return p, err
	}
	p.Workflow = w
	diags := w.Validate()
	diags = append(diags, CheckCalls(w, spec.ProjectDir, spec.file())...)
	if diags.HasErrors() {
		diags.Sort()
		return p, diags
	}
	p.Warnings = diags

	if spec.ProjectDir != "" {
		if p.Project, err = model.LoadProject(spec.ProjectDir); err != nil {
			return p, err
		}
		if p.Env, err = p.Project.SelectEnvironment(spec.Env); err != nil {
			return p, err
		}
	} else if spec.Env != "" {
		return p, fmt.Errorf("environment %s: no project (%s) found for %s", spec.Env, model.ProjectFile, spec.Name)
	}

	if p.Undeclared, err = checkParams(w.Parameters, spec.Params); err != nil {
		return p, err
	}
	if spec.strict && len(p.Undeclared) > 0 {
		return p, fmt.Errorf("parameter(s) %v not declared by workflow %q (declared: %s)", p.Undeclared, w.Name, declared(w))
	}

	var defaultJoin model.JoinMode
	if p.Project != nil {
		defaultJoin = p.Project.Workflow.DefaultJoin
	}
	envName := ""
	if p.Env != nil {
		envName = p.Env.Name
	}
	p.Runner, err = workflow.Prepare(w, workflow.Options{
		Registry:    spec.Registry,
		DefaultJoin: defaultJoin,
		Scope:       model.Scope(w, p.Project, p.Env, spec.Params),
		Observer:    spec.Observer,
		RunID:       spec.RunID,
		Environment: envName,
		Workflows:   spec.workflows(envName),
		Pipelines:   spec.pipelines(envName),
		Request:     spec.Request,
		Respond:     spec.Respond,
	})
	return p, err
}

// declared lists the parameter names of a workflow for messages.
func declared(w *model.Workflow) string { return declaredOf(w.Parameters) }

// declaredOf lists parameter names for messages; "none" without parameters.
func declaredOf(params []model.Parameter) string {
	if len(params) == 0 {
		return "none"
	}
	names := make([]string, len(params))
	for i, p := range params {
		names[i] = p.Name
	}
	return strings.Join(names, ", ")
}

// ErrMissingParameters is wrapped by the error of Prepare when required parameters are missing.
var ErrMissingParameters = errors.New("missing required parameter(s)")

// checkParams fails for missing required parameters and values that do not match
// the declared type or allowed values. It returns the undeclared parameters, sorted.
func checkParams(parameters []model.Parameter, params map[string]string) ([]string, error) {
	declared := map[string]bool{}
	var problems, missing []string
	for _, p := range parameters {
		declared[p.Name] = true
		value, given := params[p.Name]
		if !given {
			if p.Required && p.Default == "" {
				missing = append(missing, p.Name)
			}
			continue
		}
		if _, err := p.CheckValue(value); err != nil {
			problems = append(problems, fmt.Sprintf("parameter %s (%s): %v", p.Name, p.Type.OrString(), err))
		}
	}
	if len(missing) > 0 {
		prefix := ""
		if len(problems) > 0 {
			prefix = strings.Join(problems, "; ") + "; "
		}
		return nil, fmt.Errorf("%s%w %v", prefix, ErrMissingParameters, missing)
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	var undeclared []string
	for name := range params {
		if !declared[name] {
			undeclared = append(undeclared, name)
		}
	}
	slices.Sort(undeclared)
	return undeclared, nil
}
