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
)

// Spec describes a run to prepare.
type Spec struct {
	// Name identifies the workflow file in messages, e.g. "workflows/import.workflow.yaml".
	Name string
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
	Observer func(workflow.Event)
	RunID    string
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
	if diags.HasErrors() {
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

	if p.Undeclared, err = checkParams(w, spec.Params); err != nil {
		return p, err
	}

	var defaultJoin model.JoinMode
	if p.Project != nil {
		defaultJoin = p.Project.Workflow.DefaultJoin
	}
	p.Runner, err = workflow.Prepare(w, workflow.Options{
		Registry:    spec.Registry,
		DefaultJoin: defaultJoin,
		Scope:       model.Scope(w, p.Project, p.Env, spec.Params),
		Observer:    spec.Observer,
		RunID:       spec.RunID,
	})
	return p, err
}

// ErrMissingParameters is wrapped by the error of Prepare when required parameters are missing.
var ErrMissingParameters = errors.New("missing required parameter(s)")

// checkParams fails for missing required parameters and values that do not match
// the declared type or allowed values. It returns the undeclared parameters, sorted.
func checkParams(w *model.Workflow, params map[string]string) ([]string, error) {
	declared := map[string]bool{}
	var problems, missing []string
	for _, p := range w.Parameters {
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
