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
	s, err := schema.For(d.Config)
	if err != nil {
		panic(fmt.Sprintf("connection type %s: %v", d.Type, err))
	}
	r.defs[d.Type] = d
	r.desc[d.Type] = Descriptor{Type: d.Type, Label: d.Label, Description: d.Description, Schema: s}
}

// Descriptor returns the description of a type.
func (r *Registry) Descriptor(typ string) (Descriptor, bool) {
	d, ok := r.desc[typ]
	return d, ok
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
		return fmt.Errorf("unknown connection type %q (available: %s)", typ, strings.Join(r.Types(), ", "))
	}
	if with == nil {
		with = map[string]any{}
	}
	return decode(with, newConfig(d.Config))
}

// Decode is Check for the resolved fields, filling the configuration struct of the type.
func (r *Registry) Decode(c Resolved, v any) error {
	if err := r.Check(c.Type, c.With); err != nil {
		return fmt.Errorf("connection %s: %w", c.Name, err)
	}
	return decode(c.With, v)
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

// ErrNotAvailable is returned where connections are not supported (e.g. without a project).
var ErrNotAvailable = errors.New("connections are not available here (the workflow has no project)")
