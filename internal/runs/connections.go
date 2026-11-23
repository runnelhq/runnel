package runs

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/schema"
	"github.com/runnelhq/runnel/internal/vars"
)

// connectionTypes are the built-in connection types (ADR 0022); plugins bring none.
var connectionTypes = connections.Builtin()

// connectionResolver resolves the connections of a run for its environment: variables of
// the project and environment, secrets from the stores of the environment (added to the
// mask of the run, like the values of ${env:…} in the connection, ADR 0022).
func connectionResolver(p *model.Project, env *model.Environment, mask *vars.Mask) connection.Resolver {
	return func(_ context.Context, name string) (connection.Resolved, error) {
		if p == nil {
			return connection.Resolved{}, connection.ErrNotAvailable
		}
		c := p.Connections[name]
		if c == nil {
			return connection.Resolved{}, fmt.Errorf("%s", model.UnknownConnection(p, name))
		}
		if err := connectionTypes.Check(c.Type, c.With); err != nil {
			return connection.Resolved{}, fmt.Errorf("connection %s: %w", name, err)
		}
		scope := model.ConnectionScope(p, env).Scope
		scope.LookupSecret = mask.Lookup(scope.LookupSecret)
		with := make(map[string]any, len(c.With))
		for field, v := range c.With {
			text, ok := v.(string)
			if !ok {
				with[field] = v
				continue
			}
			expand := scope.Expand
			if connectionTypes.SecretField(c.Type, field) {
				expand = scope.ExpandSecret
			}
			x, err := expand(text)
			if err != nil {
				return connection.Resolved{}, fmt.Errorf("connection %s: %s: %w", name, field, err)
			}
			maskEnv(mask, text)
			with[field] = x
		}
		return connection.Resolved{Name: name, Type: c.Type, With: with}, nil
	}
}

// maskEnv adds the values of the environment variables a connection uses to the mask.
func maskEnv(mask *vars.Mask, text string) {
	refs, _ := vars.Parse(text)
	for _, r := range refs {
		if r.Kind == vars.KindEnv {
			mask.Add(os.Getenv(r.Name))
		}
	}
}

// CheckConnections checks the connections of a project (runnel validate, the editor):
// type and fields of each, secrets only in their fields for secrets and from stores
// that can be read, and their variables in every environment (or only env).
func CheckConnections(p *model.Project, env string) model.Errors {
	if p == nil {
		return nil
	}
	var errs model.Errors
	for _, name := range p.ConnectionNames() {
		errs = append(errs, CheckConnection(p.Connections[name], p, env)...)
	}
	errs.Sort()
	return errs
}

// CheckConnection checks one connection against its type and the project (p may be nil).
func CheckConnection(c *model.Connection, p *model.Project, env string) model.Errors {
	var errs model.Errors
	if _, ok := connectionTypes.Descriptor(c.Type); !ok {
		return model.Errors{c.ErrorAt("$.type", "unknown connection type %q (available: %s)", c.Type, strings.Join(connectionTypes.Types(), ", "))}
	}
	if err := connectionTypes.Check(c.Type, c.With); err != nil {
		path := "$.with"
		if f := fieldOf(err.Error()); f != "" && c.With[f] != nil {
			path += "." + f
		}
		errs = append(errs, c.ErrorAt(path, "%s connection: %v", c.Type, err))
	}
	errs = append(errs, c.CheckSecrets(func(field string) bool { return connectionTypes.SecretField(c.Type, field) }, model.SecretStoresOf(p, env))...)
	if contexts, err := model.ConnectionContexts(p, env); err == nil {
		errs = append(errs, c.CheckVariables(contexts)...)
	}
	return errs
}

// fieldOf returns the field named in a message like `field "path": …` or `unknown field "x"`.
func fieldOf(msg string) string {
	if _, rest, ok := strings.Cut(msg, `field "`); ok {
		if f, _, ok := strings.Cut(rest, `"`); ok {
			return f
		}
	}
	return ""
}

// connectionFields returns the fields of an action type that name a connection, with
// the connection types they accept.
func connectionFields(reg *action.Registry, typ string) map[string][]string {
	if reg == nil {
		return nil
	}
	d, ok := reg.Descriptor(typ)
	if !ok || d.Schema == nil {
		return nil
	}
	var out map[string][]string
	for _, p := range d.Schema.Properties {
		if p.Schema.Format == schema.FormatConnection {
			if out == nil {
				out = map[string][]string{}
			}
			out[p.Name] = p.Schema.Connection
		}
	}
	return out
}

// checkConnectionRefs reports connections named by actions that the project does not
// have or whose type does not fit; names with ${…} or {{ … }} are checked when the run
// starts (warning, like dynamic targets).
func checkConnectionRefs(w *model.Workflow, p *model.Project, reg *action.Registry) model.Errors {
	var errs model.Errors
	for i, a := range w.Actions {
		for field, types := range connectionFields(reg, a.Type) {
			name, ok := a.With[field].(string)
			if !ok || name == "" {
				continue
			}
			path := fmt.Sprintf("$.actions[%d].with.%s", i, field)
			if strings.Contains(name, "${") || strings.Contains(name, "{{") {
				errs = append(errs, w.WarningAt(path, "action %q: the connection is only known when the run starts; it is checked then", a.ID))
				continue
			}
			c := p.Connections[name]
			switch {
			case c == nil:
				errs = append(errs, w.ErrorAt(path, "action %q: %s", a.ID, model.UnknownConnection(p, name)))
			case len(types) > 0 && !slices.Contains(types, c.Type):
				errs = append(errs, w.ErrorAt(path, "action %q: connection %q is of type %s; %s needs %s", a.ID, name, c.Type, a.Type, strings.Join(types, " or ")))
			}
		}
	}
	return errs
}

// ActionSQLField tells whether a field of an action type holds an SQL statement
// (format runnel-sql): it must not contain ${…} or {{ … }}.
func ActionSQLField(reg *action.Registry) func(typ, field string) bool {
	return func(typ, field string) bool {
		if reg == nil {
			return false
		}
		d, ok := reg.Descriptor(typ)
		if !ok || d.Schema == nil {
			return false
		}
		for _, p := range d.Schema.Properties {
			if p.Name == field {
				return p.Schema.Format == schema.FormatSQL
			}
		}
		return false
	}
}

// resolveConnections resolves the connections named by the actions before the run
// starts: a missing connection, variable or secret stops the run before it begins, and
// the values of the secrets are in the mask before the first event. Names with ${…} or
// {{ … }} are resolved when the action runs.
func resolveConnections(w *model.Workflow, resolve connection.Resolver, reg *action.Registry) model.Errors {
	var errs model.Errors
	for i, a := range w.Actions {
		for field := range connectionFields(reg, a.Type) {
			name, ok := a.With[field].(string)
			if !ok || name == "" || strings.Contains(name, "${") || strings.Contains(name, "{{") {
				continue
			}
			if _, err := resolve(context.Background(), name); err != nil {
				errs = append(errs, w.ErrorAt(fmt.Sprintf("$.actions[%d].with.%s", i, field), "action %q: %v", a.ID, err))
			}
		}
	}
	errs.Sort()
	return errs
}
