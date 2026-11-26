// Package connection is the interface between the engine and the types of connections
// (ADR 0022): each type (e.g. sqlite) describes the fields under with: of a
// connections/<name>.yaml file by a configuration struct, like actions do (ADR 0014).
// Actions and transforms name a connection with a field of format runnel-connection and
// get it resolved (variables and secrets replaced) from their context.
package connection

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/schema"
)

// Definition describes a connection type.
type Definition struct {
	// Type is the name used in connection files, e.g. "sqlite".
	Type string
	// Label is the short display name, e.g. "SQLite" (English).
	Label string
	// Description is one short English sentence without a final period.
	Description string
	// Config is the struct the fields under with: are decoded into; fields for secrets
	// have the format runnel-secret.
	Config any
	// Warn returns warnings about the fields under with: by field, e.g. a switched off
	// certificate check (ADR 0022); nil: none.
	Warn func(with map[string]any) map[string]string
	// Schema is the schema of the fields under with: where there is no Config, as for the
	// connection types of plugins (ADR 0019, S49c).
	Schema *schema.Schema
	// Plugin names the plugin that provides the type; empty for built-in types.
	Plugin string
}

// Descriptor is the description of a connection type for checks and the editor.
type Descriptor struct {
	Type        string         `json:"type"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Schema      *schema.Schema `json:"schema"`
}

// Registry holds the connection types.
type Registry struct {
	defs map[string]Definition
	desc map[string]Descriptor
	// hints explain, by namespace of a plugin, why its connection types are missing.
	hints map[string]string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{defs: map[string]Definition{}, desc: map[string]Descriptor{}}
}

// Register adds a connection type; duplicates and invalid configuration structs are
// programming errors and panic.
func (r *Registry) Register(d Definition) {
	if _, dup := r.defs[d.Type]; dup || d.Type == "" || strings.ContainsAny(d.Type, " ./") {
		panic("invalid or duplicate connection type " + d.Type)
	}
	s := d.Schema
	if s == nil {
		var err error
		if s, err = schema.For(d.Config); err != nil {
			panic(fmt.Sprintf("connection type %s: %v", d.Type, err))
		}
	}
	r.defs[d.Type] = d
	r.desc[d.Type] = Descriptor{Type: d.Type, Label: d.Label, Description: d.Description, Schema: s}
}

// With returns a copy of the registry with the connection types of the plugins of a
// project and hints by namespace for plugins whose types are missing (e.g. not trusted).
// Types that exist already are left out.
func (r *Registry) With(defs []Definition, hints map[string]string) *Registry {
	out := &Registry{defs: maps.Clone(r.defs), desc: maps.Clone(r.desc), hints: maps.Clone(r.hints)}
	for _, d := range defs {
		if _, dup := out.defs[d.Type]; !dup {
			out.Register(d)
		}
	}
	for ns, h := range hints {
		if out.hints == nil {
			out.hints = map[string]string{}
		}
		out.hints[ns] = h
	}
	return out
}

// Lookup returns the definition of a type.
func (r *Registry) Lookup(typ string) (Definition, bool) {
	d, ok := r.defs[typ]
	return d, ok
}

// Hint explains why an unknown type is missing, if it belongs to a plugin of the project
// that cannot be used (its namespace, or the namespace and "-…"); empty otherwise.
func (r *Registry) Hint(typ string) string {
	for ns, h := range r.hints {
		if typ == ns || strings.HasPrefix(typ, ns+"-") {
			return h
		}
	}
	return ""
}

// Descriptor returns the description of a type.
func (r *Registry) Descriptor(typ string) (Descriptor, bool) {
	d, ok := r.desc[typ]
	return d, ok
}

// Descriptors returns the descriptions of all types, sorted by type.
func (r *Registry) Descriptors() []Descriptor {
	out := make([]Descriptor, 0, len(r.desc))
	for _, t := range r.Types() {
		out = append(out, r.desc[t])
	}
	return out
}

// Types returns the registered types, sorted.
func (r *Registry) Types() []string {
	return slices.Sorted(maps.Keys(r.defs))
}

// SecretField reports whether a field under with: of a type holds a secret.
func (r *Registry) SecretField(typ, field string) bool {
	d, ok := r.desc[typ]
	if !ok || d.Schema == nil {
		return false
	}
	for _, p := range d.Schema.Properties {
		if p.Name == field {
			return p.Schema.Format == schema.FormatSecret
		}
	}
	return false
}

// Check decodes the fields under with: of a connection of type typ into its
// configuration struct: unknown fields, wrong types and missing required fields are
// errors. Texts with ${…} are accepted as they are.
func (r *Registry) Check(typ string, with map[string]any) error {
	d, ok := r.defs[typ]
	if !ok {
		if hint := r.Hint(typ); hint != "" {
			return errors.New(hint)
		}
		return fmt.Errorf("unknown connection type %q (available: %s)", typ, strings.Join(r.Types(), ", "))
	}
	if with == nil {
		with = map[string]any{}
	}
	if d.Config == nil {
		return schema.CheckConfig(r.desc[typ].Schema, with) // a type of a plugin: it checks the values itself
	}
	return decode(r.asText(typ, with), newConfig(d.Config))
}

// Warnings returns the warnings of the type about the fields under with:, by field.
func (r *Registry) Warnings(typ string, with map[string]any) map[string]string {
	d, ok := r.defs[typ]
	if !ok || d.Warn == nil {
		return nil
	}
	return d.Warn(with)
}

// Decode is Check for the resolved fields, filling the configuration struct of the type.
func (r *Registry) Decode(c Resolved, v any) error {
	if err := r.Check(c.Type, c.With); err != nil {
		return fmt.Errorf("connection %s: %w", c.Name, err)
	}
	return decode(r.asText(c.Type, c.With), v)
}

// asText writes numbers and true/false as text where the type expects text, e.g.
// "port: 1433" for a field that may also hold "${DB_PORT}" (like actions).
func (r *Registry) asText(typ string, with map[string]any) map[string]any {
	d, ok := r.desc[typ]
	if !ok || d.Schema == nil {
		return with
	}
	out := maps.Clone(with)
	for _, p := range d.Schema.Properties {
		switch v := out[p.Name].(type) {
		case nil, string, map[string]any, []any:
		default:
			if p.Schema.Type == "string" {
				out[p.Name] = fmt.Sprint(v)
			}
		}
	}
	return out
}

// Resolved is a connection ready for use: its type and its fields with variables and
// secrets replaced. It must not be logged.
type Resolved struct {
	Name string
	Type string
	With map[string]any
}

// Resolver returns the connection of a name for the current run and environment.
type Resolver func(ctx context.Context, name string) (Resolved, error)

type resolverKey struct{}

// WithResolver returns ctx with a resolver for the connections of the project, for steps
// that get no Output or ExecContext, e.g. "Detect fields" of a transform of a plugin.
func WithResolver(ctx context.Context, r Resolver) context.Context {
	return context.WithValue(ctx, resolverKey{}, r)
}

// ResolverFrom returns the resolver set by WithResolver; nil without one.
func ResolverFrom(ctx context.Context) Resolver {
	r, _ := ctx.Value(resolverKey{}).(Resolver)
	return r
}

// ErrNotAvailable is returned where connections are not supported (e.g. without a project).
var ErrNotAvailable = errors.New("connections are not available here (the workflow has no project)")
