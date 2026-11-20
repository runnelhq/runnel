package runs

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/model"
)

// ActionTypeRunWorkflow is the type of the action that calls another workflow.
const ActionTypeRunWorkflow = "core.run-workflow"

// CheckCalls checks the calls of other workflows (core.run-workflow) whose path is
// fixed (ADR 0008): the file exists and can be read, the parameters are declared,
// required ones are given and fixed values fit, and no workflow calls itself, also
// not through others. Paths with ${…} or {{ … }} are checked when the action runs.
// projectDir and file are as in Spec (file: the workflow file on disk).
func CheckCalls(w *model.Workflow, projectDir, file string) model.Errors {
	var errs model.Errors
	for i, a := range w.Actions {
		path, ok := staticText(a.With["workflow"])
		if a.Type != ActionTypeRunWorkflow || !ok || path == "" {
			continue
		}
		at := fmt.Sprintf("$.actions[%d].with", i)
		childFile, name := Resolve(projectDir, file, path)
		child, err := readWorkflow(childFile, name)
		if err != nil {
			errs = append(errs, w.ErrorAt(at+".workflow", "action %q: %v", a.ID, err))
			continue
		}
		params, _ := a.With["params"].(map[string]any)
		errs = append(errs, checkParamsOf(w, at, a.ID, child, name, params)...)
		if chain := callsBack(projectDir, absolute(file), w.Name, childFile, name, map[string]bool{}); chain != nil {
			errs = append(errs, w.ErrorAt(at+".workflow", "action %q: the workflow calls itself (%s)",
				a.ID, strings.Join(append([]string{w.Name}, chain...), " → ")))
		}
	}
	return errs
}

// staticText returns a value as text if it is fixed, i.e. contains no ${…} or {{ … }}.
func staticText(v any) (string, bool) {
	var s string
	switch v := v.(type) {
	case string:
		s = v
	case nil:
		return "", false
	default:
		s = expr.Format(v)
	}
	if strings.Contains(s, "${") || expr.Contains(s) {
		return "", false
	}
	return s, true
}

func readWorkflow(file, name string) (*model.Workflow, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("workflow file %s not found", name)
	} else if err != nil {
		return nil, fmt.Errorf("cannot read workflow file %s: %w", name, err)
	}
	w, err := model.ParseWorkflow(name, data)
	if err != nil {
		return nil, fmt.Errorf("workflow file %s has errors; check it with \"runnel validate\"", name)
	}
	return w, nil
}

func checkParamsOf(w *model.Workflow, at, id string, child *model.Workflow, name string, params map[string]any) model.Errors {
	var errs model.Errors
	declaredParams := map[string]model.Parameter{}
	for _, p := range child.Parameters {
		declaredParams[p.Name] = p
		if _, given := params[p.Name]; !given && p.Required && p.Default == "" {
			errs = append(errs, w.ErrorAt(at, "action %q: missing required parameter %s of workflow %s", id, p.Name, name))
		}
	}
	for _, key := range slices.Sorted(maps.Keys(params)) {
		p, ok := declaredParams[key]
		if !ok {
			errs = append(errs, w.ErrorAt(at+".params."+key, "action %q: parameter %s is not declared by workflow %s (declared: %s)",
				id, key, name, declared(child)))
			continue
		}
		if text, fixed := staticText(params[key]); fixed {
			if _, err := p.CheckValue(text); err != nil {
				errs = append(errs, w.ErrorAt(at+".params."+key, "action %q: parameter %s (%s): %v", id, key, p.Type.OrString(), err))
			}
		}
	}
	return errs
}

// callsBack follows the fixed calls of a workflow file and returns the names of the
// workflows along the way if they lead back to root; nil otherwise.
func callsBack(projectDir, root, rootName, file, name string, seen map[string]bool) []string {
	abs := absolute(file)
	if abs == root {
		return []string{rootName}
	}
	if seen[abs] {
		return nil
	}
	seen[abs] = true
	w, err := readWorkflow(file, name)
	if err != nil {
		return nil
	}
	for _, a := range w.Actions {
		path, ok := staticText(a.With["workflow"])
		if a.Type != ActionTypeRunWorkflow || !ok || path == "" {
			continue
		}
		childFile, childName := Resolve(projectDir, file, path)
		if chain := callsBack(projectDir, root, rootName, childFile, childName, seen); chain != nil {
			return append([]string{w.Name}, chain...)
		}
	}
	return nil
}
