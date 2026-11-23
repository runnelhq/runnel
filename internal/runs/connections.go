package runs

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/schema"
	"github.com/runnelhq/runnel/internal/transform"
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
	warnings := connectionTypes.Warnings(c.Type, c.With)
	for _, field := range slices.Sorted(maps.Keys(warnings)) {
		errs = append(errs, c.WarningAt("$.with."+field, "%s", warnings[field]))
	}
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

// steps are the actions of a workflow or the transforms of a pipeline, for the checks
// and the resolution of the connections they name.
type steps struct {
	list, noun         string // "actions"/"action" or "transforms"/"transform"
	ids, types         []string
	withs              []map[string]any
	schema             func(typ string) *schema.Schema
	errorAt, warningAt func(path, format string, args ...any) *model.Error
}

func workflowSteps(w *model.Workflow, reg *action.Registry) steps {
	s := steps{list: "actions", noun: "action", errorAt: w.ErrorAt, warningAt: w.WarningAt,
		schema: func(typ string) *schema.Schema {
			if reg == nil {
				return nil
			}
			d, _ := reg.Descriptor(typ)
			return d.Schema
		}}
	for _, a := range w.Actions {
		s.ids, s.types, s.withs = append(s.ids, a.ID), append(s.types, a.Type), append(s.withs, a.With)
	}
	return s
}

func pipelineSteps(pl *model.Pipeline, reg *transform.Registry) steps {
	s := steps{list: "transforms", noun: "transform", errorAt: pl.ErrorAt, warningAt: pl.WarningAt,
		schema: func(typ string) *schema.Schema {
			if reg == nil {
				return nil
			}
			d, _ := reg.Descriptor(typ)
			return d.Schema
		}}
	for _, t := range pl.Transforms {
		s.ids, s.types, s.withs = append(s.ids, t.ID), append(s.types, t.Type), append(s.withs, t.With)
	}
	return s
}

// connections calls fn for every fixed or dynamic connection name of the steps, with the
// connection types its field accepts.
func (s steps) connections(fn func(i int, path, name string, types []string)) {
	for i, typ := range s.types {
		sc := s.schema(typ)
		if sc == nil {
			continue
		}
		for _, p := range sc.Properties {
			if p.Schema.Format != schema.FormatConnection {
				continue
			}
			if name, ok := s.withs[i][p.Name].(string); ok && name != "" {
				fn(i, fmt.Sprintf("$.%s[%d].with.%s", s.list, i, p.Name), name, p.Schema.Connection)
			}
		}
	}
}

func dynamicName(name string) bool {
	return strings.Contains(name, "${") || strings.Contains(name, "{{")
}

// checkConnectionRefs reports connections named by the steps that the project does not
// have or whose type does not fit; names with ${…} or {{ … }} are checked when the run
// starts (warning, like dynamic targets).
func checkConnectionRefs(s steps, p *model.Project) model.Errors {
	var errs model.Errors
	s.connections(func(i int, path, name string, types []string) {
		id := s.ids[i]
		if dynamicName(name) {
			errs = append(errs, s.warningAt(path, "%s %q: the connection is only known when the run starts; it is checked then", s.noun, id))
			return
		}
		c := p.Connections[name]
		switch {
		case c == nil:
			errs = append(errs, s.errorAt(path, "%s %q: %s", s.noun, id, model.UnknownConnection(p, name)))
		case len(types) > 0 && !slices.Contains(types, c.Type):
			errs = append(errs, s.errorAt(path, "%s %q: connection %q is of type %s; %s needs %s", s.noun, id, name, c.Type, s.types[i], strings.Join(types, " or ")))
		}
	})
	return errs
}

// resolveConnections resolves the connections named by the steps before the run starts:
// a missing connection, variable or secret stops the run before it begins, and the
// values of the secrets are in the mask before the first event. Names with ${…} or
// {{ … }} are resolved when the step runs.
func resolveConnections(s steps, resolve connection.Resolver) model.Errors {
	var errs model.Errors
	s.connections(func(i int, path, name string, _ []string) {
		if dynamicName(name) {
			return
		}
		if _, err := resolve(context.Background(), name); err != nil {
			errs = append(errs, s.errorAt(path, "%s %q: %v", s.noun, s.ids[i], err))
		}
	})
	errs.Sort()
	return errs
}

// formatField tells whether a field of a step type has a format, e.g. runnel-sql.
func formatField(schemaOf func(typ string) *schema.Schema, format string) func(typ, field string) bool {
	return func(typ, field string) bool {
		sc := schemaOf(typ)
		if sc == nil {
			return false
		}
		for _, p := range sc.Properties {
			if p.Name == field {
				return p.Schema.Format == format
			}
		}
		return false
	}
}

// ActionSQLField tells whether a field of an action type holds an SQL statement
// (format runnel-sql): it must not contain ${…} or {{ … }}.
func ActionSQLField(reg *action.Registry) func(typ, field string) bool {
	return formatField(workflowSteps(&model.Workflow{}, reg).schema, schema.FormatSQL)
}

// TransformSQLField is ActionSQLField for transforms.
func TransformSQLField(reg *transform.Registry) func(typ, field string) bool {
	return formatField(pipelineSteps(&model.Pipeline{}, reg).schema, schema.FormatSQL)
}
