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
	"strconv"
	"strings"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/schema"
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
	// RunWorkflow runs another workflow of the project (core.run-workflow) and returns
	// its outputs; nil where the caller of the engine does not support it.
	RunWorkflow func(ctx context.Context, path string, params map[string]string) (map[string]any, error)
	// RunPipeline runs a pipeline of the project (core.run-pipeline) and returns its
	// result (the counts of its transforms and its result records); nil where it is
	// not supported. rows are passed to core.from-result.
	RunPipeline func(ctx context.Context, path string, params map[string]string, rows []map[string]any) (map[string]any, error)
	// Respond answers the waiting caller of an http trigger (http.respond, S38b1); nil
	// if the run has no waiting caller.
	Respond Responder
}

// Response is the answer of a run to the waiting caller of an http trigger.
type Response struct {
	Status  int
	Headers map[string]string
	// Body is sent as JSON, or as text if it is a string; json.RawMessage is sent as it is.
	Body any
	// Fallback marks an answer that is sent only if the run ends without another one
	// (http.response with whenEmpty, S38c); it does not take the place of a later answer.
	Fallback bool
}

// ParseStatus reads the HTTP status of an answer (http.respond, http.response): a
// number from 200 to 599; empty is 200.
func ParseStatus(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 200, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 200 || n > 599 {
		return 0, fmt.Errorf("invalid status %q (expected a number from 200 to 599)", s)
	}
	return n, nil
}

// Responder delivers the answer of a run to the waiting caller of an http trigger and
// reports whether it was the first one: only the first answer is sent.
type Responder func(Response) bool

// WorkflowCall is a call of another workflow from an action (core.run-workflow).
type WorkflowCall struct {
	// Path of the workflow file as written in the action (relative to the project).
	Path   string
	Params map[string]string
	// Log writes to the log of the calling action.
	Log func(level slog.Level, msg string)
}

// WorkflowRunner runs a called workflow and returns its outputs (see ExecContext.RunWorkflow).
type WorkflowRunner func(ctx context.Context, call WorkflowCall) (map[string]any, error)

// PipelineCall is a call of a pipeline from an action (core.run-pipeline).
type PipelineCall struct {
	WorkflowCall
	// Rows are records for core.from-result in the pipeline (S24a).
	Rows []map[string]any
}

// PipelineRunner runs a pipeline called by an action and returns its result (see
// ExecContext.RunPipeline).
type PipelineRunner func(ctx context.Context, call PipelineCall) (map[string]any, error)

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
	// Output is data for the following actions, e.g. a list of files; expressions
	// read it as result.output (ADR 0006). Texts, numbers, booleans, lists and maps.
	Output any
}

// Categories group actions in the editor's palette (ADR 0014). The editor shows
// them in this order and translates the identifiers into display names.
const (
	CategoryGeneral   = "general"
	CategoryExecution = "execution"
	CategoryFiles     = "files"
	CategoryNetwork   = "network"
	CategoryDatabase  = "database"
)

// Categories lists all categories in palette order.
var Categories = []string{CategoryGeneral, CategoryExecution, CategoryFiles, CategoryNetwork, CategoryDatabase}

// Definition describes an action type.
type Definition struct {
	// Type is the name used in workflow files, e.g. "core.log".
	Type string
	// Category is one of Categories.
	Category string
	// Label is the short display name in the editor, e.g. "Log" (English, title case).
	Label string
	// Description is one short English sentence without a final period.
	Description string
	// Example is a short configuration ("with:", in YAML) shown in the help of the editor;
	// empty only for actions without configuration. The contract test checks that New
	// accepts it.
	Example string
	// Config is the configuration struct that New decodes "with" into, e.g. logAction{};
	// nil for actions without configuration. The JSON Schema of the action is derived
	// from it (package schema).
	Config any
	// New creates an action from its "with" configuration. It is called once before
	// the run with the configuration as written (to report unknown or missing fields
	// early) and again before each execution with variables resolved. New must only
	// check the structure; values that may contain variables are checked in Execute.
	New func(with map[string]any) (Action, error)
}

// Descriptor describes an action type for the editor, the API (/catalog) and tools
// (ADR 0014). Plugins will provide the same structure.
type Descriptor struct {
	Type        string         `json:"type"`
	Kind        string         `json:"kind"` // always "action"; transforms use "transform"
	Category    string         `json:"category"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Schema      *schema.Schema `json:"schema"` // schema of the "with" configuration
	// Example: see Definition.Example.
	Example string `json:"example,omitempty"`
}

// Registry holds the available action types. Transforms have a registry of their own,
// so that the same type name may exist as action and as transform (ADR 0013).
type Registry struct {
	defs        map[string]Definition
	descriptors map[string]Descriptor
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{defs: map[string]Definition{}, descriptors: map[string]Descriptor{}}
}

// Register adds an action type. It panics on invalid type names, duplicates and
// configuration structs without a valid schema, which are programming errors.
func (r *Registry) Register(d Definition) {
	if !model.ValidTypeName(d.Type) {
		panic(fmt.Sprintf("invalid action type name %q (expected \"<namespace>.<name>\")", d.Type))
	}
	if _, dup := r.defs[d.Type]; dup {
		panic("action type registered twice: " + d.Type)
	}
	s, err := schema.For(d.Config)
	if err != nil {
		panic(fmt.Sprintf("action type %s: %v", d.Type, err))
	}
	r.defs[d.Type] = d
	r.descriptors[d.Type] = Descriptor{
		Type:        d.Type,
		Kind:        "action",
		Category:    d.Category,
		Label:       d.Label,
		Description: d.Description,
		Schema:      s,
		Example:     d.Example,
	}
}

// Lookup returns the definition of an action type.
func (r *Registry) Lookup(typ string) (Definition, bool) {
	d, ok := r.defs[typ]
	return d, ok
}

// Descriptor returns the descriptor of an action type.
func (r *Registry) Descriptor(typ string) (Descriptor, bool) {
	d, ok := r.descriptors[typ]
	return d, ok
}

// Descriptors returns the descriptors of all action types, sorted by type.
func (r *Registry) Descriptors() []Descriptor {
	out := make([]Descriptor, 0, len(r.descriptors))
	for _, typ := range r.Types() {
		out = append(out, r.descriptors[typ])
	}
	return out
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
// Unknown fields and missing required fields (tag schema:"required") are errors.
// Field names are those of the json tags.
func DecodeConfig(with map[string]any, v any) error {
	return decodeConfig(with, v, false)
}

// DecodeConfigNumbers is DecodeConfig, but numbers in fields of type any are decoded as
// json.Number instead of float64, so that whole numbers stay whole (e.g. constant
// values of pipeline transforms; see record.Normalize).
func DecodeConfigNumbers(with map[string]any, v any) error {
	return decodeConfig(with, v, true)
}

func decodeConfig(with map[string]any, v any, useNumber bool) error {
	data, err := json.Marshal(with)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if useNumber {
		dec.UseNumber()
	}
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
	if field := schema.MissingRequired(v, with); field != "" {
		return fmt.Errorf("missing required field %q", field)
	}
	return nil
}

// Func adapts a function to the Action interface.
type Func func(ctx context.Context, ec *ExecContext) (Result, error)

// Execute calls f.
func (f Func) Execute(ctx context.Context, ec *ExecContext) (Result, error) {
	return f(ctx, ec)
}
