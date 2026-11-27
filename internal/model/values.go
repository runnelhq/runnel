package model

import (
	"os"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/vars"
)

// ExprValues returns what expressions see as vars (all variables, resolved) and params
// (the declared parameters, converted to their type; ADR 0006). A date becomes a time at
// midnight, local time, as for fields of records, so that it works with today(),
// formatDate() and < >. Workflows and pipelines share it.
func ExprValues(scope *vars.Scope, params []Parameter) (variables, parameters map[string]any) {
	variables = map[string]any{}
	for _, name := range scope.Names() {
		if v, err := scope.Expand("${" + name + "}"); err == nil {
			variables[name] = v
		}
	}
	parameters = map[string]any{}
	for _, p := range params {
		text, ok := variables[p.Name].(string)
		if !ok {
			continue
		}
		if v, err := p.Type.Parse(text); err == nil {
			if d, ok := v.(record.Date); ok {
				v = d.In(time.Local)
			}
			parameters[p.Name] = v
		} else {
			parameters[p.Name] = text
		}
	}
	return variables, parameters
}

// Environ returns the environment variables of the system, as expressions see them (env).
func Environ() map[string]any {
	env := map[string]any{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
			env[k] = v
		}
	}
	return env
}
