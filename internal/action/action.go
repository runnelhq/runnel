// Package action defines the interface between the workflow engine and the actions
// it runs, and the registry of available action types.
package action

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/model"
)

// Action is one configured step, ready to run.
type Action interface {
	// Execute runs the action. A returned error means the action failed;
	// the workflow then follows failure links. ctx is cancelled on abort or timeout.
	Execute(ctx context.Context, ec *ExecContext) (Result, error)
}

// ExecContext gives an action access to its run.
type ExecContext struct {
	RunID    string
	ActionID string
	// Log writes a message to the run log.
	Log func(level slog.Level, msg string)
}

// Logf writes a formatted message to the run log.
func (ec *ExecContext) Logf(level slog.Level, format string, args ...any) {
	if ec.Log != nil {
		ec.Log(level, fmt.Sprintf(format, args...))
	}
}

// End tells the engine whether the workflow continues after an action.
type End int

// Values of End.
const (
	// Continue follows the links of the action (the default).
	Continue End = iota
	// EndSuccess ends the workflow successfully.
	EndSuccess
	// EndFailure ends the workflow as failed, without following failure links.
	EndFailure
)

// Result is the outcome of a successful Execute.
type Result struct {
	End End
	// Message explains an EndSuccess or EndFailure.
	Message string
}

// Definition describes an action type.
type Definition struct {
	// Type is the name used in workflow files, e.g. "core.log".
	Type        string
	Description string
	// New creates an action from its "with" configuration. It is called once before
	// the run with the configuration as written (to report unknown or missing fields
	// early) and again before each execution with variables resolved. New must only
	// check the structure; values that may contain variables are checked in Execute.
	New func(with map[string]any) (Action, error)
}

// Registry holds the available action types. Transforms have a registry of their own,
// so that the same type name may exist as action and as transform (ADR 0013).
type Registry struct {
	defs map[string]Definition
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{defs: map[string]Definition{}}
}

// Register adds an action type. It panics on invalid type names and duplicates,
// which are programming errors.
func (r *Registry) Register(d Definition) {
	if !model.ValidTypeName(d.Type) {
		panic(fmt.Sprintf("invalid action type name %q (expected \"<namespace>.<name>\")", d.Type))
	}
	if _, dup := r.defs[d.Type]; dup {
		panic("action type registered twice: " + d.Type)
	}
	r.defs[d.Type] = d
}

// Lookup returns the definition of an action type.
func (r *Registry) Lookup(typ string) (Definition, bool) {
	d, ok := r.defs[typ]
	return d, ok
}

// Types returns all registered type names, sorted.
func (r *Registry) Types() []string {
	types := make([]string, 0, len(r.defs))
	for t := range r.defs {
		types = append(types, t)
	}
	slices.Sort(types)
	return types
}

// DecodeConfig decodes a "with" configuration into the struct pointed to by v.
// Unknown fields are an error. Field names are those of the json tags.
func DecodeConfig(with map[string]any, v any) error {
	data, err := json.Marshal(with)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			return fmt.Errorf("field %q: expected %s, got %s", typeErr.Field, typeErr.Type.Kind(), typeErr.Value)
		}
		if field, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
			return fmt.Errorf("unknown field %s", field)
		}
		return err
	}
	return nil
}

// Func adapts a function to the Action interface.
type Func func(ctx context.Context, ec *ExecContext) (Result, error)

// Execute calls f.
func (f Func) Execute(ctx context.Context, ec *ExecContext) (Result, error) {
	return f(ctx, ec)
}
