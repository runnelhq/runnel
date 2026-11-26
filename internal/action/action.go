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
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/runnelhq/runnel/internal/connection"
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
	// Connection returns a connection of the project (connections/<name>.yaml, ADR 0022)
	// with variables and secrets resolved for the environment of the run; nil where
	// connections are not supported. Its values must not be logged.
	Connection connection.Resolver
	// RunWorkflow runs another workflow of the project (core.run-workflow) and returns
	// its outputs; nil where the caller of the engine does not support it.
	RunWorkflow func(ctx context.Context, path string, params map[string]string) (map[string]any, error)
	// RunPipeline runs a pipeline of the project (core.run-pipeline) and returns its
	// result (the counts of its transforms and its result records); nil where it is
	// not supported. rows are passed to core.from-result.
	RunPipeline func(ctx context.Context, path string, params map[string]string, rows []map[string]any) (map[string]any, error)
	// Respond answers the waiting caller of an http trigger (http.response, S38b1); nil
	// if the run has no waiting caller.
	Respond Responder
	// Plugins runs actions of plugins in their processes of this run (ADR 0019); nil
	// where plugins are not supported.
	Plugins PluginRunner
}

// PluginRunner runs the actions of plugins in the processes of one run (ADR 0019): one
// process per plugin, started on first use and ended by Close at the end of the run.
type PluginRunner interface {
	Execute(ctx context.Context, plugin, typ string, config map[string]any, ec *ExecContext) (any, error)
	Close()
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

// ParseStatus reads the HTTP status of an answer (http.response as action and transform): a
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
	// Category is one of Categories; empty for a type of a plugin that names none of
	// them (the editor groups it by its plugin).
	Category string
	// Plugin is the name of the plugin the type comes from; empty for built-in types.
	Plugin string
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
	// Schema is the schema of the configuration where it does not come from Config, as
	// for actions of plugins (describe, ADR 0019).
	Schema *schema.Schema
	// Reference documents the output, the errors and the limits of the action (E15b).
	Reference
	// New creates an action from its "with" configuration. It is called once before
	// the run with the configuration as written (to report unknown or missing fields
	// early) and again before each execution with variables resolved. New must only
	// check the structure; values that may contain variables are checked in Execute.
	New func(with map[string]any) (Action, error)
	// Deprecated lists renamed and deprecated fields of the configuration (ADR 0029):
	// Register maps them to their new names before New and marks them in the schema.
	Deprecated []Deprecated
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
	// Plugin: see Definition.Plugin.
	Plugin string `json:"plugin,omitempty"`
	Reference
}

// Reference documents what a type gives, how it fails and its limits, for the catalog,
// the website and the help of the editor (roadmap E15b). Transforms use it too; plugins
// may describe the same keys (ADR 0019). Texts are English sentences with a final period.
type Reference struct {
	// Outputs are, for an action, the parts of result.output by their path, e.g.
	// "output.status" or "output[].path" ("output" for the whole value); for a
	// transform, the fields it adds or changes. A name from the configuration is written
	// as the path of its field in angle brackets, e.g. "<name>" or "<fields[].name>".
	Outputs []OutputField `json:"outputs,omitempty"`
	// Records tells, for a transform only, which records it passes on and over which
	// links (data links, targets, error link).
	Records string `json:"records,omitempty"`
	// Errors tells when the type fails and, for a transform, which records go to the
	// error link; also what ifExists and ifMissing do.
	Errors string `json:"errors,omitempty"`
	// Limits are limits of size, number and time, with their defaults.
	Limits []string `json:"limits,omitempty"`
}

// OutputField is a part of the output of an action or a field set by a transform.
type OutputField struct {
	Name string `json:"name"`
	// Type is one of OutputTypes for actions, a record type (or "any") for transforms.
	Type        string `json:"type"`
	Description string `json:"description"`
}

// OutputTypes are the types of the output of actions, as expressions see it.
var OutputTypes = []string{"string", "integer", "number", "boolean", "datetime", "list", "object", "any"}

// Registry holds the available action types. Transforms have a registry of their own,
// so that the same type name may exist as action and as transform (ADR 0013).
type Registry struct {
	defs        map[string]Definition
	descriptors map[string]Descriptor
	// hints explain, by namespace, why types of a plugin are missing (e.g. not trusted).
	hints map[string]string
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
	s := d.Schema
	if s == nil {
		var err error
		if s, err = schema.For(d.Config); err != nil {
			panic(fmt.Sprintf("action type %s: %v", d.Type, err))
		}
	}
	if len(d.Deprecated) > 0 {
		if err := DeprecateSchema(s, d.Deprecated); err != nil {
			panic(fmt.Sprintf("action type %s: %v", d.Type, err))
		}
		newFn, deps := d.New, d.Deprecated
		d.New = func(with map[string]any) (Action, error) {
			with, err := MapDeprecated(with, deps)
			if err != nil {
				return nil, err
			}
			return newFn(with)
		}
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
		Plugin:      d.Plugin,
		Reference:   d.Reference,
	}
}

// With returns a copy of the registry with more action types (those of the plugins of a
// project) and hints by namespace for types that are missing, e.g. of a plugin that is
// not trusted. The definitions must be valid (checked by the caller).
func (r *Registry) With(defs []Definition, hints map[string]string) *Registry {
	out := &Registry{defs: maps.Clone(r.defs), descriptors: maps.Clone(r.descriptors), hints: maps.Clone(r.hints)}
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

// Hint explains why an unknown type is missing, if its namespace belongs to a plugin
// of the project that cannot be used; empty otherwise.
func (r *Registry) Hint(typ string) string {
	return r.hints[model.TypeNamespace(typ)]
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

// Renamed returns an error if with has the field old, renamed to use before 1.0 (R5b):
// a clear message instead of "unknown field". It is a clean break; renames after 1.0
// go through Deprecated (ADR 0029).
func Renamed(with map[string]any, old, use string) error {
	if _, ok := with[old]; ok {
		return fmt.Errorf("field %q was renamed before 1.0: use %s", old, use)
	}
	return nil
}

// DecodeConfig decodes a "with" configuration into the struct pointed to by v.
// Unknown fields and missing required fields (tag schema:"required") are errors.
// Field names are those of the json tags. Fields of type any, map[string]any and []any
// get the values as they are, so that exact numbers and dates keep their type.
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
		if typeErr, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
			return fmt.Errorf("field %q: expected %s, got %s", typeErr.Field, typeErr.Type.Kind(), typeErr.Value)
		}
		if field, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
			return fmt.Errorf("unknown field %s", field)
		}
		return err
	}
	if !useNumber {
		keepValues(with, v)
	}
	if field := schema.MissingRequired(v, with); field != "" {
		return fmt.Errorf("missing required field %q", field)
	}
	return nil
}

// keepValues puts the values of the run back into the fields of type any, map[string]any
// and []any: decoding goes through JSON, which turns exact decimals into text in quotes,
// whole numbers beyond 2^53 into rounded floats and dates into text. Only fields of
// the struct itself, not of nested structs.
func keepValues(with map[string]any, v any) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.Elem().Kind() != reflect.Struct {
		return
	}
	rv = rv.Elem()
	for i := range rv.NumField() {
		f := rv.Type().Field(i)
		if !f.IsExported() || !keepsValue(f.Type) {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		raw, ok := with[name]
		if name == "" || name == "-" || !ok {
			continue
		}
		if raw == nil {
			rv.Field(i).SetZero()
		} else if t := reflect.TypeOf(raw); t.AssignableTo(f.Type) {
			rv.Field(i).Set(reflect.ValueOf(raw))
		}
	}
}

var (
	anyType  = reflect.TypeFor[any]()
	mapType  = reflect.TypeFor[map[string]any]()
	listType = reflect.TypeFor[[]any]()
)

func keepsValue(t reflect.Type) bool { return t == anyType || t == mapType || t == listType }

// Func adapts a function to the Action interface.
type Func func(ctx context.Context, ec *ExecContext) (Result, error)

// Execute calls f.
func (f Func) Execute(ctx context.Context, ec *ExecContext) (Result, error) {
	return f(ctx, ec)
}
