package model

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
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
	Kind               string            `yaml:"kind" json:"kind" doc:"Type of the file" schema:"required,enum=Project"`
	APIVersion         string            `yaml:"apiVersion" json:"apiVersion" doc:"Version of the file format" schema:"required,enum=runnel/v1"`
	Name               string            `yaml:"name" json:"name" doc:"Name of the project" schema:"required,example=my-project"`
	Description        string            `yaml:"description,omitempty" json:"description,omitempty" doc:"What the project does; shown in the editor"`
	DefaultEnvironment string            `yaml:"defaultEnvironment,omitempty" json:"defaultEnvironment,omitempty" doc:"Environment used when a run names none, e.g. dev; the name of a file in environments/" schema:"example=dev"`
	Variables          map[string]string `yaml:"variables,omitempty" json:"variables,omitempty" doc:"Variables for all environments, used as ${NAME}; an environment can override them. Values may use other variables, e.g. ${BASE}\\inbox"`
	SecretStore        string            `yaml:"secretStore,omitempty" json:"secretStore,omitempty" doc:"Where ${secret:NAME} reads secrets from, unless an environment sets its own: env (environment variables), inline (secrets: in the file, plain text) or local (the local secret store of the account, set with runnel secret set); empty: local" schema:"enum=env|inline|local"`
	Secrets            map[string]string `yaml:"secrets,omitempty" json:"-" doc:"Secrets in plain text for secretStore: inline, used as ${secret:NAME} where no environment has its own; only for development and simple APIs"`
	// Settings control the runs of the project as a whole (roadmap S37) and hold the
	// defaults for its workflows (R5b, before: the block workflow).
	Settings ProjectSettings `yaml:"settings,omitempty" json:"settings,omitzero" doc:"Defaults and limits for the runs of the project"`

	// Dir is the project directory as passed to LoadProject.
	Dir string `yaml:"-" json:"-"`
	// Environments by name (file name without extension).
	Environments map[string]*Environment `yaml:"-" json:"environments,omitempty"`
	// Connections by name (files in connections/, ADR 0022); those without errors.
	Connections map[string]*Connection `yaml:"-" json:"-"`
	// Plugins by name (plugins/<name>/plugin.yaml, ADR 0019); those without errors.
	Plugins map[string]*Plugin `yaml:"-" json:"-"`

	src *source
}

// ProjectSettings control the runs of a project.
type ProjectSettings struct {
	// DefaultJoin applies to actions without "join" (ADR 0003); empty means all. Until
	// R5b it was workflow.defaultJoin.
	DefaultJoin JoinMode `yaml:"defaultJoin,omitempty" json:"defaultJoin,omitempty" doc:"When an action with several incoming links starts, if it sets no join: all (every link decided), first or each" schema:"enum=all|first|each,default=all"`
	// MaxConcurrentRuns limits the runs of the server that run at the same time; a
	// start beyond it is rejected (KONZEPT, chapter 6.4). 0: no limit.
	MaxConcurrentRuns int `yaml:"maxConcurrentRuns,omitempty" json:"maxConcurrentRuns,omitempty" doc:"Most runs of the project at the same time on the server; a start beyond it is rejected. 0: no limit" schema:"default=0"`
	// History controls the run history of the project (ADR 0026).
	History HistorySettings `yaml:"history,omitempty" json:"history,omitzero" doc:"Run history of the project: which runs are kept"`
	// OnError is the workflow started after a failed run (ADR 0027), relative to the project.
	OnError string `yaml:"onError,omitempty" json:"onError,omitempty" doc:"Workflow started when a run of the project fails (not for runs in the editor), relative to the project; it gets the FAILED_… parameters it declares. A workflow or pipeline can set its own or none" schema:"example=workflows/on-error.workflow.yaml"`
}

// Limits of the run history (ADR 0026, S26c).
const (
	DefaultHistoryDays = 30
	DefaultHistoryRuns = 10000
	MaxHistoryDays     = 3650
	MaxHistoryRuns     = 1000000
)

// Retention returns how long and how many runs the history keeps, with the defaults.
func (h HistorySettings) Retention() (days, runs int) {
	days, runs = h.KeepDays, h.MaxRuns
	if days <= 0 {
		days = DefaultHistoryDays
	}
	if runs <= 0 {
		runs = DefaultHistoryRuns
	}
	return days, runs
}

// HistorySettings control the run history of a project (ADR 0026, S26b, S26c).
type HistorySettings struct {
	// KeepDays and MaxRuns limit the history (S26c): older runs and those beyond the
	// newest MaxRuns are removed. 0: the default.
	KeepDays int `yaml:"keepDays,omitempty" json:"keepDays,omitempty" doc:"Days ended runs are kept in the run history; older ones are removed" schema:"default=30"`
	MaxRuns  int `yaml:"maxRuns,omitempty" json:"maxRuns,omitempty" doc:"Most runs kept in the run history of the project; the oldest beyond it are removed" schema:"default=10000"`
	// CLI chooses which runs of runnel run are kept: all, errors or none.
	CLI string `yaml:"cli,omitempty" json:"cli,omitempty" doc:"Which ended runs of runnel run are kept in the run history: all, errors (only failed ones) or none; runnel run --history overrides it" schema:"enum=all|errors|none,default=all"`
}

// Environment is the content of environments/<name>.yaml: variables for one target system.
type Environment struct {
	Kind        string            `yaml:"kind" json:"kind" doc:"Type of the file" schema:"required,enum=Environment"`
	APIVersion  string            `yaml:"apiVersion" json:"apiVersion" doc:"Version of the file format" schema:"required,enum=runnel/v1"`
	Name        string            `yaml:"name" json:"name" doc:"Name of the environment; must match the file name" schema:"required,example=prod"`
	Description string            `yaml:"description,omitempty" json:"description,omitempty" doc:"What the environment is for, e.g. the production system"`
	Variables   map[string]string `yaml:"variables,omitempty" json:"variables,omitempty" doc:"Variables of this environment, used as ${NAME}; they override those of project.yaml"`
	SecretStore string            `yaml:"secretStore,omitempty" json:"secretStore,omitempty" doc:"Where ${secret:NAME} reads secrets from in this environment, instead of secretStore of project.yaml: env (environment variables), inline (secrets: in this file, plain text) or local (the local secret store of the account, set with runnel secret set)" schema:"enum=env|inline|local"`
	Secrets     map[string]string `yaml:"secrets,omitempty" json:"-" doc:"Secrets of this environment in plain text for secretStore: inline, used as ${secret:NAME}; only for development and simple APIs"`

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
		if es, ok := errors.AsType[Errors](err); ok {
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

	conns, err := LoadConnections(dir)
	if err := collect(err); err != nil {
		return nil, err
	}
	plugins, err := LoadPlugins(dir)
	if err := collect(err); err != nil {
		return nil, err
	}

	if p != nil {
		p.Dir = dir
		p.Environments = envs
		p.Connections = conns
		p.Plugins = plugins
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
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ReadEnvironment(filepath.ToSlash(path), data)
}

// ReadEnvironment parses and checks an environment file; name is the file (its base name
// must match the name of the environment) and is used in messages, e.g. the text shown
// in the editor (E10).
func ReadEnvironment(name string, data []byte) (*Environment, error) {
	src, err := parseSource(name, data)
	if err != nil {
		return nil, err
	}
	path := name
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
	errs = append(errs, checkSecretStore(src, env.SecretStore)...)
	errs = append(errs, checkSecretNames(src, env.Secrets)...)
	errs = append(errs, checkSecrets(src, nil, nil)...)
	if err := errs.errOrNil(); err != nil {
		return nil, err
	}
	return env, nil
}

// ReadProject parses and checks project.yaml from data, e.g. the text shown in the editor
// (E10); name is used in messages. envExists reports whether an environment exists, for
// defaultEnvironment (nil: not checked).
func ReadProject(name string, data []byte, envExists func(env string) bool) (*Project, error) {
	src, err := parseSource(name, data)
	if err != nil {
		return nil, err
	}
	p := &Project{src: src}
	if err := src.decode(p); err != nil {
		return nil, err
	}
	errs := p.validate()
	if d := p.DefaultEnvironment; d != "" && envExists != nil && !envExists(d) {
		errs = append(errs, src.errorf("$.defaultEnvironment", "default environment %q not found (expected %s/%s.yaml)", d, EnvironmentsDir, d))
	}
	return p, errs.errOrNil()
}

func (p *Project) validate() Errors {
	errs := checkHeader(p.src, p.Kind, KindProject, p.APIVersion, p.Name)
	switch p.Settings.DefaultJoin {
	case "", JoinAll, JoinFirst, JoinEach:
	default:
		errs = append(errs, p.src.errorf("$.settings.defaultJoin", "invalid defaultJoin %q (expected all, first or each)", p.Settings.DefaultJoin))
	}
	if p.Settings.MaxConcurrentRuns < 0 {
		errs = append(errs, p.src.errorf("$.settings.maxConcurrentRuns", "maxConcurrentRuns must not be negative"))
	}
	if h := p.Settings.History; h.KeepDays < 0 || h.KeepDays > MaxHistoryDays {
		errs = append(errs, p.src.errorf("$.settings.history.keepDays", "keepDays must be between 1 and %d (0 or missing: %d)", MaxHistoryDays, DefaultHistoryDays))
	}
	if h := p.Settings.History; h.MaxRuns < 0 || h.MaxRuns > MaxHistoryRuns {
		errs = append(errs, p.src.errorf("$.settings.history.maxRuns", "maxRuns must be between 1 and %d (0 or missing: %d)", MaxHistoryRuns, DefaultHistoryRuns))
	}
	switch p.Settings.History.CLI {
	case "", HistoryAll, HistoryErrors, HistoryNone:
	default:
		errs = append(errs, p.src.errorf("$.settings.history.cli", "invalid history %q (expected all, errors or none)", p.Settings.History.CLI))
	}
	if p.Settings.OnError == OnErrorNone {
		errs = append(errs, p.src.errorf("$.settings.onError", "onError: none is only for workflows and pipelines (to turn off the setting of the project); leave settings.onError out instead"))
	} else if msg := CheckOnErrorPath(p.Settings.OnError); msg != "" {
		errs = append(errs, p.src.errorf("$.settings.onError", "%s", msg))
	}
	errs = append(errs, checkVariables(p.src, p.Variables)...)
	errs = append(errs, checkReferenceSyntax(p.src)...)
	errs = append(errs, checkSecretStore(p.src, p.SecretStore)...)
	errs = append(errs, checkSecretNames(p.src, p.Secrets)...)
	errs = append(errs, checkSecrets(p.src, nil, nil)...)
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

// ProjectDirVariable is the built-in variable with the absolute path of the project
// folder (ADR 0020), e.g. for paths put together in expressions. A variable of the same
// name in the project, an environment or the parameters takes precedence.
const ProjectDirVariable = "PROJECT_DIR"

// BuiltinLayer is the name of the variable layer of the built-in variables (the lowest).
const BuiltinLayer = "runnel"

// absDir returns dir as an absolute path, or as it is if that fails.
func absDir(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// ParameterDefaultsLayer is the name of the variable layer with the default values of
// the workflow parameters (lowest but for the built-in variables).
const ParameterDefaultsLayer = "workflow parameters"

// Scope returns the variables available to a workflow, from lowest to highest priority:
// the built-in PROJECT_DIR (with a project), defaults of the workflow parameters, project variables, environment variables and
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
	var layers []vars.Layer
	if p != nil && p.Dir != "" {
		layers = append(layers, vars.Layer{Name: BuiltinLayer, Values: map[string]string{ProjectDirVariable: absDir(p.Dir)}})
	}
	layers = append(layers, vars.Layer{Name: ParameterDefaultsLayer, Values: defaults})
	if p != nil {
		layers = append(layers, vars.Layer{Name: "project", Values: p.Variables})
	}
	if env != nil {
		layers = append(layers, vars.Layer{Name: "environment " + env.Name, Values: env.Variables})
	}
	layers = append(layers, vars.Layer{Name: "parameters", Values: params})
	s := vars.NewScope(layers...)
	s.LookupSecret = SecretLookup(p, env)
	return s
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

// OnErrorNone in settings.onError of a workflow or pipeline turns off the error workflow
// of the project for it (ADR 0027).
const OnErrorNone = "none"

// CheckOnErrorPath checks the syntax of settings.onError (ADR 0027): a path relative to
// the project (ADR 0020) of a workflow file. It returns a message, or "" if it is fine.
// Whether the file exists is checked against the project (runs.CheckOnError).
func CheckOnErrorPath(value string) string {
	if value == "" || value == OnErrorNone {
		return ""
	}
	switch {
	case strings.Contains(value, "${") || strings.Contains(value, "{{"):
		return fmt.Sprintf("onError %q: variables and expressions are not allowed here; give the path of a workflow of the project", value)
	case filepath.IsAbs(value) || strings.HasPrefix(slash(value), "/") || len(value) > 1 && value[1] == ':' ||
		slices.Contains(strings.Split(slash(value), "/"), ".."):
		return fmt.Sprintf("onError %q must be a path inside the project, relative to its directory (e.g. workflows/on-error.workflow.yaml)", value)
	case !strings.HasSuffix(value, ".workflow.yaml"):
		return fmt.Sprintf("onError %q must be a workflow file (*.workflow.yaml), not a pipeline or another file", value)
	}
	return ""
}

// OnErrorTarget returns the workflow to start after a failed run (ADR 0027): the
// setting of the file (none: no workflow), else that of the project; "" if none or if
// the setting is invalid (CheckOnErrorPath), e.g. of a project with errors. The path is
// relative to the project with "/".
func OnErrorTarget(project *Project, file string) string {
	if file == "" && project != nil {
		file = project.Settings.OnError
	}
	if file == "" || file == OnErrorNone || CheckOnErrorPath(file) != "" {
		return ""
	}
	return path.Clean(slash(file))
}

// slash returns a path with "/" as separator, also for "\" on Linux (files written on Windows).
func slash(p string) string { return strings.ReplaceAll(p, "\\", "/") }

// ErrorAt creates an error located at a YAML path of project.yaml.
func (p *Project) ErrorAt(path, format string, args ...any) *Error {
	if p.src == nil {
		return &Error{Msg: fmt.Sprintf(format, args...)}
	}
	return p.src.errorf(path, format, args...)
}

// WarningAt creates a warning located at a YAML path of project.yaml.
func (p *Project) WarningAt(path, format string, args ...any) *Error {
	if p.src == nil {
		return &Error{Severity: SeverityWarning, Msg: fmt.Sprintf(format, args...)}
	}
	return p.src.warnf(path, format, args...)
}
