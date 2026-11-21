package model

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/vars"
)

// Project layout.
const (
	ProjectFile     = "project.yaml"
	EnvironmentsDir = "environments"
)

// Project is the content of project.yaml plus the environments of the project.
type Project struct {
	Kind               string            `yaml:"kind" json:"kind"`
	APIVersion         string            `yaml:"apiVersion" json:"apiVersion"`
	Name               string            `yaml:"name" json:"name"`
	Description        string            `yaml:"description,omitempty" json:"description,omitempty"`
	DefaultEnvironment string            `yaml:"defaultEnvironment,omitempty" json:"defaultEnvironment,omitempty"`
	Variables          map[string]string `yaml:"variables,omitempty" json:"variables,omitempty"`
	// Workflow holds project-wide defaults for workflows.
	Workflow ProjectWorkflowSettings `yaml:"workflow,omitempty" json:"workflow,omitzero"`
	// Settings control the runs of the project as a whole (roadmap S37).
	Settings ProjectSettings `yaml:"settings,omitempty" json:"settings,omitzero"`

	// Dir is the project directory as passed to LoadProject.
	Dir string `yaml:"-" json:"-"`
	// Environments by name (file name without extension).
	Environments map[string]*Environment `yaml:"-" json:"environments,omitempty"`

	src *source
}

// ProjectWorkflowSettings are project-wide defaults for workflows.
type ProjectWorkflowSettings struct {
	// DefaultJoin applies to actions without "join" (ADR 0003); empty means all.
	DefaultJoin JoinMode `yaml:"defaultJoin,omitempty" json:"defaultJoin,omitempty"`
}

// ProjectSettings control the runs of a project.
type ProjectSettings struct {
	// MaxConcurrentRuns limits the runs of the server that run at the same time; a
	// start beyond it is rejected (KONZEPT, chapter 6.4). 0: no limit.
	MaxConcurrentRuns int `yaml:"maxConcurrentRuns,omitempty" json:"maxConcurrentRuns,omitempty"`
}

// Environment is the content of environments/<name>.yaml: variables for one target system.
type Environment struct {
	Kind        string            `yaml:"kind" json:"kind"`
	APIVersion  string            `yaml:"apiVersion" json:"apiVersion"`
	Name        string            `yaml:"name" json:"name"`
	Description string            `yaml:"description,omitempty" json:"description,omitempty"`
	Variables   map[string]string `yaml:"variables,omitempty" json:"variables,omitempty"`

	src *source
}

// FindProjectDir searches start and its parent directories for project.yaml
// and returns the absolute path of the directory containing it.
func FindProjectDir(start string) (string, bool, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false, err
	}
	for {
		info, err := os.Stat(filepath.Join(dir, ProjectFile))
		switch {
		case err == nil && !info.IsDir():
			return dir, true, nil
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return "", false, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false, nil
		}
		dir = parent
	}
}

// LoadProject reads project.yaml and all environments of the project in dir.
// Problems in the files are returned as Errors, covering all files at once.
func LoadProject(dir string) (*Project, error) {
	var errs Errors
	collect := func(err error) error {
		var es Errors
		if errors.As(err, &es) {
			errs = append(errs, es...)
			return nil
		}
		return err
	}

	p := &Project{}
	src, err := readSource(filepath.Join(dir, ProjectFile))
	if err == nil {
		err = src.decode(p)
	}
	if err != nil {
		if err := collect(err); err != nil {
			return nil, err
		}
		p = nil
	} else {
		p.src = src
		errs = append(errs, p.validate()...)
	}

	envs := map[string]*Environment{}
	files, err := filepath.Glob(filepath.Join(dir, EnvironmentsDir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	for _, file := range files {
		env, err := loadEnvironment(file)
		if err != nil {
			if err := collect(err); err != nil {
				return nil, err
			}
			continue
		}
		envs[env.Name] = env
	}

	if p != nil {
		p.Dir = dir
		p.Environments = envs
		if d := p.DefaultEnvironment; d != "" && envs[d] == nil && !hasFileErrors(errs, environmentFile(dir, d)) {
			errs = append(errs, p.src.errorf("$.defaultEnvironment", "default environment %q not found (expected %s)",
				d, environmentFile(dir, d)))
		}
	}
	if len(errs) > 0 {
		return p, errs.errOrNil()
	}
	return p, nil
}

func environmentFile(dir, name string) string {
	return filepath.ToSlash(filepath.Join(dir, EnvironmentsDir, name+".yaml"))
}

func hasFileErrors(errs Errors, file string) bool {
	return slices.ContainsFunc(errs, func(e *Error) bool { return e.File == file })
}

func readSource(path string) (*source, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseSource(filepath.ToSlash(path), data)
}

func loadEnvironment(path string) (*Environment, error) {
	src, err := readSource(path)
	if err != nil {
		return nil, err
	}
	env := &Environment{src: src}
	if err := src.decode(env); err != nil {
		return nil, err
	}
	errs := checkHeader(src, env.Kind, KindEnvironment, env.APIVersion, env.Name)
	if want := strings.TrimSuffix(filepath.Base(path), ".yaml"); env.Name != "" && env.Name != want {
		errs = append(errs, src.errorf("$.name", "environment name %q must match the file name (%q)", env.Name, want))
	}
	errs = append(errs, checkVariables(src, env.Variables)...)
	errs = append(errs, checkReferenceSyntax(src)...)
	if err := errs.errOrNil(); err != nil {
		return nil, err
	}
	return env, nil
}

func (p *Project) validate() Errors {
	errs := checkHeader(p.src, p.Kind, KindProject, p.APIVersion, p.Name)
	switch p.Workflow.DefaultJoin {
	case "", JoinAll, JoinFirst, JoinEach:
	default:
		errs = append(errs, p.src.errorf("$.workflow.defaultJoin", "invalid defaultJoin %q (expected all, first or each)", p.Workflow.DefaultJoin))
	}
	if p.Settings.MaxConcurrentRuns < 0 {
		errs = append(errs, p.src.errorf("$.settings.maxConcurrentRuns", "maxConcurrentRuns must not be negative"))
	}
	errs = append(errs, checkVariables(p.src, p.Variables)...)
	errs = append(errs, checkReferenceSyntax(p.src)...)
	return errs
}

func checkVariables(s *source, values map[string]string) Errors {
	var errs Errors
	for _, name := range slices.Sorted(maps.Keys(values)) {
		if !vars.ValidName(name) {
			errs = append(errs, s.errorf(keyPath("$.variables", name),
				"invalid variable name %q (letters, digits and _, not starting with a digit)", name))
		}
	}
	return errs
}

// SelectEnvironment returns the environment with the given name, or the default
// environment if name is empty. It returns nil (and no error) if neither is set.
func (p *Project) SelectEnvironment(name string) (*Environment, error) {
	if name == "" {
		name = p.DefaultEnvironment
	}
	if name == "" {
		return nil, nil
	}
	if env := p.Environments[name]; env != nil {
		return env, nil
	}
	return nil, fmt.Errorf("environment %q not found in project %q (available: %s)",
		name, p.Name, strings.Join(p.EnvironmentNames(), ", "))
}

// EnvironmentNames returns the names of all environments, sorted.
func (p *Project) EnvironmentNames() []string {
	return slices.Sorted(maps.Keys(p.Environments))
}

// ParameterDefaultsLayer is the name of the variable layer with the default values of
// the workflow parameters (the lowest layer of Scope).
const ParameterDefaultsLayer = "workflow parameters"

// Scope returns the variables available to a workflow, from lowest to highest priority:
// defaults of the workflow parameters, project variables, environment variables and
// parameter values passed at start (see docs/KONZEPT.md, chapter 2.5).
// p, env and params may be nil.
func Scope(w *Workflow, p *Project, env *Environment, params map[string]string) *vars.Scope {
	return scopeFor(w.Parameters, p, env, params)
}

// PipelineScope is Scope for a pipeline.
func PipelineScope(pl *Pipeline, p *Project, env *Environment, params map[string]string) *vars.Scope {
	return scopeFor(pl.Parameters, p, env, params)
}

func scopeFor(parameters []Parameter, p *Project, env *Environment, params map[string]string) *vars.Scope {
	defaults := map[string]string{}
	for _, param := range parameters {
		defaults[param.Name] = param.Default
	}
	layers := []vars.Layer{{Name: ParameterDefaultsLayer, Values: defaults}}
	if p != nil {
		layers = append(layers, vars.Layer{Name: "project", Values: p.Variables})
	}
	if env != nil {
		layers = append(layers, vars.Layer{Name: "environment " + env.Name, Values: env.Variables})
	}
	layers = append(layers, vars.Layer{Name: "parameters", Values: params})
	return vars.NewScope(layers...)
}

// VarContexts returns the variable contexts to check a workflow against: one per
// environment of the project, or only the environment named only (if not empty).
// p may be nil (workflow without project); params are the values passed at start.
func VarContexts(w *Workflow, p *Project, only string, params map[string]string) ([]VarContext, error) {
	if p == nil {
		return []VarContext{{Scope: Scope(w, nil, nil, params)}}, nil
	}
	envs := p.EnvironmentNames()
	if only != "" {
		if _, err := p.SelectEnvironment(only); err != nil {
			return nil, err
		}
		envs = []string{only}
	}
	if len(envs) == 0 {
		return []VarContext{{Scope: Scope(w, p, nil, params)}}, nil
	}
	contexts := make([]VarContext, len(envs))
	for i, name := range envs {
		contexts[i] = VarContext{Environment: name, Scope: Scope(w, p, p.Environments[name], params)}
	}
	return contexts, nil
}
