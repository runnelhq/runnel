package runs

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/model"
)

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
	file := filepath.Join(projectDir, filepath.FromSlash(t.Target))
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return append(errs, t.ErrorAt("$.target", "target %s not found in the project", t.Target))
	} else if err != nil {
		return append(errs, t.ErrorAt("$.target", "cannot read target %s: %v", t.Target, err))
	}
	var params []model.Parameter
	what := "workflow " + t.Target
	if strings.HasSuffix(t.Target, ".pipeline.yaml") {
		what = "pipeline " + t.Target
		pl, err := model.ParsePipeline(t.Target, data)
		if err != nil {
			return append(errs, t.ErrorAt("$.target", "%s has errors; check it with \"runnel validate\"", what))
		}
		params = pl.Parameters
	} else {
		w, err := model.ParseWorkflow(t.Target, data)
		if err != nil {
			return append(errs, t.ErrorAt("$.target", "%s has errors; check it with \"runnel validate\"", what))
		}
		params = w.Parameters
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
	return append(errs, checkGivenParams(t.ErrorAt, "$", "", params, what, given)...)
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
		return model.Errors{t.ErrorAt("$.with.param", "parameter %s is not declared by %s (declared: %s)", name, what, declaredOf(params))}
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
		if s, ok := staticText(v); ok {
			out[k] = s
		} else {
			out[k] = fmt.Sprint(v) // ${…} is resolved like a value given at start
		}
	}
	return out
}
