package workflow

import (
	"fmt"
	"maps"
	"os"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/vars"
)

// runContext holds what expressions can use during a run (ADR 0006): vars, params,
// env, run and the results of the actions so far. It belongs to the scheduler's goroutine.
type runContext struct {
	// scope resolves ${…} references; nil if the runner has none (texts stay as they are).
	scope   *vars.Scope
	vars    map[string]any
	params  map[string]any
	env     map[string]any
	run     map[string]any
	results map[string]any
}

// newRunContext evaluates expressions in parameter defaults and prepares the context.
func (r *Runner) newRunContext(runID string, started time.Time) (*runContext, error) {
	// Without a scope, vars and params come from the parameter defaults alone.
	scope := r.opts.Scope
	if scope == nil {
		scope = model.Scope(r.w, nil, nil, nil)
	}
	c := &runContext{
		env: environ(),
		run: map[string]any{
			"id": runID, "workflow": r.w.Name, "environment": r.opts.Environment,
			"startedAt": started, "lastError": "",
		},
		results: map[string]any{},
	}

	// Defaults of parameters that were not given may be expressions; they are evaluated
	// once, in the order of declaration, and replace the text in the variables.
	defaults := map[string]string{}
	computed := false
	for _, p := range r.w.Parameters {
		defaults[p.Name] = p.Default
		_, layer, _ := scope.Lookup(p.Name)
		if layer != model.ParameterDefaultsLayer || !expr.Contains(p.Default) {
			continue
		}
		c.fill(scope.ReplaceLayer(model.ParameterDefaultsLayer, defaults), r.w.Parameters)
		v, err := r.evalText(p.Default, r.expandWith(scope), c.values(nil))
		if err != nil {
			return nil, fmt.Errorf("parameter %q: default value: %w", p.Name, err)
		}
		defaults[p.Name] = expr.Format(v)
		computed = true
	}
	if computed {
		scope = scope.ReplaceLayer(model.ParameterDefaultsLayer, defaults)
	}
	c.fill(scope, r.w.Parameters)
	c.scope = r.expandWith(scope)
	return c, nil
}

// expandWith returns the scope for resolving ${…} references: nil if the runner has none.
func (r *Runner) expandWith(scope *vars.Scope) *vars.Scope {
	if r.opts.Scope == nil {
		return nil
	}
	return scope
}

// fill sets vars (all variables, resolved) and params (declared parameters with their type).
func (c *runContext) fill(scope *vars.Scope, params []model.Parameter) {
	c.vars = map[string]any{}
	for _, name := range scope.Names() {
		if v, err := scope.Expand("${" + name + "}"); err == nil {
			c.vars[name] = v
		}
	}
	c.params = map[string]any{}
	for _, p := range params {
		text, ok := c.vars[p.Name].(string)
		if !ok {
			continue
		}
		if v, err := p.Type.Parse(text); err == nil {
			c.params[p.Name] = v
		} else {
			c.params[p.Name] = text
		}
	}
}

// values returns the context for one evaluation; result is the result of the action
// the expression refers to (nil if none). Maps are copied, so that an action running
// in its own goroutine is not affected by later results.
func (c *runContext) values(result any) map[string]any {
	return map[string]any{
		"vars":    c.vars,
		"params":  c.params,
		"env":     c.env,
		"run":     maps.Clone(c.run),
		"results": maps.Clone(c.results),
		"result":  result,
	}
}

// record stores the result of an action for the following expressions.
func (c *runContext) record(a *model.Action, status Status, res action.Result, err error, d time.Duration) map[string]any {
	v := map[string]any{
		"id": a.ID, "type": a.Type, "status": string(status), "success": status == Succeeded,
		"output": res.Output, "durationMs": d.Milliseconds(), "error": "",
	}
	if err != nil {
		v["error"] = err.Error()
		c.run["lastError"] = err.Error()
	}
	c.results[a.ID] = v
	return v
}

func environ() map[string]any {
	env := map[string]any{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
			env[k] = v
		}
	}
	return env
}

// template returns the compiled template of a text; templates are compiled once per runner.
func (r *Runner) template(text string) (*expr.Template, error) {
	if t, ok := r.templates.Load(text); ok {
		return t.(*expr.Template), nil
	}
	t, err := expr.Compile(text, expr.WorkflowRoots)
	if err != nil {
		return nil, err
	}
	r.templates.Store(text, t)
	return t, nil
}

// evalText resolves ${…} references and then evaluates {{ … }} expressions (ADR 0006).
// A text that is one expression keeps the type of its result.
func (r *Runner) evalText(text string, scope *vars.Scope, values map[string]any) (any, error) {
	if scope != nil {
		var err error
		if text, err = scope.Expand(text); err != nil {
			return nil, err
		}
	}
	if !expr.Contains(text) {
		return text, nil
	}
	t, err := r.template(text)
	if err != nil {
		return nil, err
	}
	return t.Eval(values)
}

// condition evaluates the expression condition of a link; the result must be true or false.
func (r *Runner) condition(when string, scope *vars.Scope, values map[string]any) (bool, error) {
	if scope != nil {
		var err error
		if when, err = scope.Expand(when); err != nil {
			return false, err
		}
	}
	t, err := r.template(when)
	if err != nil {
		return false, err
	}
	return t.EvalBool(values)
}

// evalWith resolves variables and expressions in an action's configuration. A field
// that is text in the schema of the action gets the result as text.
func (r *Runner) evalWith(a *model.Action, scope *vars.Scope, values map[string]any) (map[string]any, error) {
	if a.With == nil {
		return nil, nil
	}
	text := map[string]bool{}
	if d, ok := r.opts.Registry.Descriptor(a.Type); ok && d.Schema != nil {
		for _, p := range d.Schema.Properties {
			text[p.Name] = p.Schema.Type == "string"
		}
	}
	out := make(map[string]any, len(a.With))
	for k, v := range a.With {
		e, err := r.evalValue(v, scope, values)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
		if _, isText := e.(string); text[k] && !isText && e != nil {
			e = expr.Format(e)
		}
		out[k] = e
	}
	return out, nil
}

func (r *Runner) evalValue(v any, scope *vars.Scope, values map[string]any) (any, error) {
	switch v := v.(type) {
	case string:
		return r.evalText(v, scope, values)
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			x, err := r.evalValue(e, scope, values)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			out[k] = x
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			x, err := r.evalValue(e, scope, values)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			out[i] = x
		}
		return out, nil
	}
	return v, nil
}
