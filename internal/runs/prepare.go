// Package runs prepares workflow runs and keeps track of the runs of a server.
//
// Prepare is shared by "runnel run" and the server, so that both accept and reject
// the same workflows, environments and parameters.
package runs

import (
	"errors"
	"fmt"
	"path/filepath"
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
	// expressions) and Respond answers it (http.response); both are passed on to
	// called workflows (S38b1).
	Request map[string]any
	Respond action.Responder
	// History decides whether the run is kept when it has ended: all (default),
	// errors or none (runs of http triggers, S38b2).
	History string
	// Parent is the run this one belongs to (Run.Parent): the failed run of an error
	// workflow (ADR 0027).
	Parent string
	// NoOnError starts no error workflow when the run fails (ADR 0027), e.g. for runs of
	// the editor or with runnel run --no-on-error.
	NoOnError bool

	// chain lists the workflows that called this one (core.run-workflow).
	chain []caller
	// strict rejects parameters the workflow does not declare (called workflows).
	strict bool
}

// BaseDir is the folder relative paths of a run refer to (ADR 0020): the project folder,
// or without a project the folder of the workflow or pipeline file.
func BaseDir(projectDir, file string) string {
	if projectDir != "" {
		return projectDir
	}
	return filepath.Dir(file)
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
	spec.Registry = WithPlugins(spec.Registry, spec.ProjectDir) // actions of plugins (S49b2)
	w, diags, err := parseWorkflow(spec.Name, spec.Data)
	if err != nil {
		return p, err
	}
	p.Workflow = w
	diags = append(diags, CheckCalls(w, spec.ProjectDir, spec.file())...)
	// ${secret:…} outside the fields for secrets, ${…} in SQL (ADR 0022) and parameters
	// or expressions in the program of shell.run (ADR 0023) stop the run before it starts.
	diags = append(diags, w.CheckSecrets(ActionSecretField(spec.Registry), nil)...)
	diags = append(diags, w.CheckSQL(ActionSQLField(spec.Registry))...)
	diags = append(diags, w.CheckProgram(ActionProgramField(spec.Registry))...)
	diags = append(diags, w.CheckScript(ActionScriptField(spec.Registry))...)
	if diags.HasErrors() {
		diags.Sort()
		return p, diags
	}
	p.Warnings = diags

	if p.Project, p.Env, err = scope(spec.ProjectDir, spec.Env, spec.Name); err != nil {
		return p, err
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
	scope, mask := secretScope(model.Scope(w, p.Project, p.Env, spec.Params))
	if errs := resolveSecrets(w, scope, spec.Registry); len(errs) > 0 {
		return p, errs
	}
	conns := connectionResolver(p.Project, p.Env, mask)
	if errs := resolveConnections(workflowSteps(w, spec.Registry), conns); len(errs) > 0 {
		return p, errs
	}
	p.Runner, err = workflow.Prepare(w, workflow.Options{
		Registry:    spec.Registry,
		DefaultJoin: defaultJoin,
		Scope:       scope,
		Mask:        mask,
		Connections: conns,
		Plugins:     pluginSession(spec.ProjectDir, envName, mask),
		Observer:    spec.Observer,
		RunID:       spec.RunID,
		Environment: envName,
		Workflows:   spec.workflows(envName),
		Pipelines:   spec.pipelines(envName),
		Request:     spec.Request,
		Respond:     spec.Respond,
		BaseDir:     BaseDir(spec.ProjectDir, spec.file()),
	})
	return p, err
}

// declared lists the parameter names of a workflow for messages.
func declared(w *model.Workflow) string { return DeclaredOf(w.Parameters) }

// DeclaredOf lists parameter names for messages; "none" without parameters.
func DeclaredOf(params []model.Parameter) string {
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

// scope loads the project of a run and selects its environment; without a project
// directory, giving an environment is an error. name names the file in messages.
func scope(projectDir, env, name string) (*model.Project, *model.Environment, error) {
	if projectDir == "" {
		if env != "" {
			return nil, nil, fmt.Errorf("environment %s: no project (%s) found for %s", env, model.ProjectFile, name)
		}
		return nil, nil, nil
	}
	p, err := loadProject(projectDir)
	if err != nil {
		return nil, nil, err
	}
	e, err := p.SelectEnvironment(env)
	return p, e, err
}

// CheckValues checks the values given at start against the declared parameters: problems
// are values that do not match the type or allowed values ("parameter X (type): …"),
// missing the required parameters without value and default. Parameters for which skip
// reports true are not checked (e.g. fixed values of a trigger).
func CheckValues(parameters []model.Parameter, values map[string]string, skip func(name string) bool) (problems, missing []string) {
	for _, p := range parameters {
		if skip != nil && skip(p.Name) {
			continue
		}
		value, given := values[p.Name]
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
	return problems, missing
}

// checkParams fails for missing required parameters and values that do not match
// the declared type or allowed values. It returns the undeclared parameters, sorted.
func checkParams(parameters []model.Parameter, params map[string]string) ([]string, error) {
	declared := map[string]bool{}
	for _, p := range parameters {
		declared[p.Name] = true
	}
	problems, missing := CheckValues(parameters, params, nil)
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
