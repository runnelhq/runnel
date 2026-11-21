package triggers

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/runs"
)

// TargetParams reads the target of a trigger and returns its parameters and a
// description such as "workflow workflows/a.workflow.yaml"; the error is located at
// the target of the trigger.
func TargetParams(t *model.Trigger, projectDir string) ([]model.Parameter, string, error) {
	params, what, err := runs.ReadParameters(projectDir, t.Target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, "", t.ErrorAt("$.target", "target %s not found in the project", t.Target)
	case errors.Is(err, runs.ErrHasErrors):
		return nil, what, t.ErrorAt("$.target", "%s has errors; check it with \"runnel validate\"", what)
	case err != nil:
		return nil, "", t.ErrorAt("$.target", "cannot read target %s: %v", t.Target, err)
	}
	return params, what, nil
}

// CheckTrigger checks what a trigger refers to (roadmap S37): the target exists in the
// project and has no errors, the environment exists and the parameters fit the target
// like those of a call (CheckCalls). p is the project of the trigger; it may be nil if
// the project could not be loaded.
func CheckTrigger(t *model.Trigger, p *model.Project, projectDir string) model.Errors {
	var errs model.Errors
	if p != nil && t.Environment != "" && p.Environments[t.Environment] == nil {
		names := p.EnvironmentNames()
		hint := "the project has no environments"
		if len(names) > 0 {
			hint = "environments: " + strings.Join(names, ", ")
		}
		errs = append(errs, t.ErrorAt("$.environment", "environment %q not found (%s)", t.Environment, hint))
	}
	params, what, err := TargetParams(t, projectDir)
	var located *model.Error
	if errors.As(err, &located) {
		return append(errs, located)
	}
	given := t.Params
	if t.Type == model.TriggerFile {
		errs = append(errs, checkFileParam(t, params, what)...)
		if slices.ContainsFunc(params, func(p model.Parameter) bool { return p.Name == t.With.Param }) {
			given = maps.Clone(t.Params)
			if given == nil {
				given = map[string]any{}
			}
			given[t.With.Param] = nil // set at start; not a fixed value to check
		}
	}
	if t.Type == model.TriggerHTTP {
		errs = append(errs, checkHTTPTrigger(t, params, what)...)
		return errs // the required parameters may come with the request
	}
	return append(errs, runs.CheckGivenParams(t.ErrorAt, "$", "", params, what, given)...)
}

// checkHTTPTrigger checks an http trigger against its target: the fixed params fit,
// each placeholder of the path is a parameter that is not fixed, and the environment
// variable of the token is set.
func checkHTTPTrigger(t *model.Trigger, params []model.Parameter, what string) model.Errors {
	var errs model.Errors
	declared := map[string]bool{}
	for _, p := range params {
		declared[p.Name] = true
	}
	for _, key := range slices.Sorted(maps.Keys(t.Params)) {
		if !declared[key] {
			errs = append(errs, t.ErrorAt("$.params."+key, "parameter %s is not declared by %s (declared: %s)", key, what, runs.DeclaredOf(params)))
		}
	}
	errs = append(errs, checkFixedValues(t, params, what)...)
	for _, name := range t.Placeholders() {
		switch {
		case !declared[name]:
			errs = append(errs, t.ErrorAt("$.with.path", "placeholder {%s} is not a parameter of %s (declared: %s)", name, what, runs.DeclaredOf(params)))
		case t.Params[name] != nil:
			errs = append(errs, t.ErrorAt("$.with.path", "placeholder {%s} is also set under params; fixed values cannot come from the request", name))
		}
	}
	if v := t.TokenVariable(); v != "" {
		if _, ok := t.Token(); !ok {
			errs = append(errs, t.ErrorAt("$.with.auth.token", "environment variable %s with the token is not set; set it for the server (for a service: as a system variable, then restart it)", v))
		}
	}
	return errs
}

// checkFixedValues checks the fixed values under params against the types of the target.
func checkFixedValues(t *model.Trigger, params []model.Parameter, what string) model.Errors {
	var errs model.Errors
	for _, p := range params {
		v, ok := t.Params[p.Name]
		if !ok {
			continue
		}
		if text, fixed := runs.StaticText(v); fixed {
			if _, err := p.CheckValue(text); err != nil {
				errs = append(errs, t.ErrorAt("$.params."+p.Name, "parameter %s (%s) of %s: %v", p.Name, p.Type.OrString(), what, err))
			}
		}
	}
	return errs
}

// checkFileParam checks the parameter that receives the path (mode each, type string)
// or the paths as a JSON list (mode batch, type json) of a file trigger.
func checkFileParam(t *model.Trigger, params []model.Parameter, what string) model.Errors {
	name := t.With.Param
	if _, ok := t.Params[name]; ok {
		return model.Errors{t.ErrorAt("$.params."+name, "parameter %s is set by the file trigger (with.param); remove it from params", name)}
	}
	i := slices.IndexFunc(params, func(p model.Parameter) bool { return p.Name == name })
	if i < 0 {
		return model.Errors{t.ErrorAt("$.with.param", "parameter %s is not declared by %s (declared: %s)", name, what, runs.DeclaredOf(params))}
	}
	want := model.TypeString
	if t.FileMode() == model.FileBatch {
		want = model.TypeJSON
	}
	if got := params[i].Type.OrString(); got != want {
		return model.Errors{t.ErrorAt("$.with.param", "parameter %s of %s has type %s; with mode: %s it gets %s, so it must have type %s",
			name, what, got, t.FileMode(), map[string]string{model.FileEach: "the path of a file", model.FileBatch: "a JSON list of paths"}[t.FileMode()], want)}
	}
	if len(params[i].Enum) > 0 {
		return model.Errors{t.ErrorAt("$.with.param", "parameter %s of %s allows only some values (enum); a file path cannot be one of them", name, what)}
	}
	return nil
}

// TriggerParams returns the parameters of a trigger as text, as given at start.
func TriggerParams(t *model.Trigger) map[string]string {
	if len(t.Params) == 0 {
		return nil
	}
	out := make(map[string]string, len(t.Params))
	for k, v := range t.Params {
		if s, ok := runs.StaticText(v); ok {
			out[k] = s
		} else {
			out[k] = fmt.Sprint(v) // ${…} is resolved like a value given at start
		}
	}
	return out
}
