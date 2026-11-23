// Package transform defines the interface between the pipeline engine and the
// transforms it runs, and the registry of transform types. Transforms have a registry
// of their own, separate from actions (ADR 0013): the same type name may exist as
// action and as transform (e.g. core.log).
package transform

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/schema"
)

// Batch is a group of records passed over a link at once (KONZEPT, chapter 6.2);
// transforms see batches only when reading, they emit single records.
type Batch []record.Record

// Transform is one configured step of a pipeline, ready to run. All transforms of a
// pipeline run at the same time, each in its own goroutine (KONZEPT, chapter 6.2).
type Transform interface {
	// Process reads the batches of in until it is closed and passes records on with
	// out.Emit. in is closed when all incoming links have ended; for a transform without
	// incoming links it is closed from the start. An error stops the whole pipeline.
	// ctx is cancelled when the pipeline is aborted; out.Emit then returns ctx.Err().
	Process(ctx context.Context, in <-chan Batch, out *Output) error
}

// Output passes the records of a transform on and writes to the run log.
type Output struct {
	id      string
	emit    func(record.Record) error
	log     func(slog.Level, string)
	values  map[string]any
	fail    func(record.Record) error
	emitTo  func(string, record.Record) error
	rows    []record.Record
	result  func(record.Record) error
	respond action.Responder
	conns   connection.Resolver
}

// NewOutput creates an Output; the engine creates one per transform, tests their own.
func NewOutput(transformID string, emit func(record.Record) error, log func(slog.Level, string)) *Output {
	return &Output{id: transformID, emit: emit, log: log}
}

// WithConnections sets how Connection resolves connections; the engine sets it.
func (o *Output) WithConnections(conns connection.Resolver) *Output {
	o.conns = conns
	return o
}

// Connection returns a connection of the project (connections/<name>.yaml, ADR 0022)
// with variables and secrets resolved for the environment of the run. Its values must
// not be logged.
func (o *Output) Connection(ctx context.Context, name string) (connection.Resolved, error) {
	if o.conns == nil {
		return connection.Resolved{}, connection.ErrNotAvailable
	}
	return o.conns(ctx, name)
}

// WithTargets sets how EmitTo reaches a single outgoing data link; the engine sets it.
func (o *Output) WithTargets(emitTo func(target string, r record.Record) error) *Output {
	o.emitTo = emitTo
	return o
}

// EmitTo passes a record to the data link that leads to the transform with the id
// target only (e.g. core.switch), instead of to all of them like Emit. The targets are
// known in Init (InitContext.Targets). Without targets set (outside the engine), it
// behaves like Emit.
func (o *Output) EmitTo(target string, r record.Record) error {
	if o.emitTo == nil {
		return o.emit(r)
	}
	return o.emitTo(target, r)
}

// WithResults sets the records passed to the run (Rows) and where AddResult collects
// the result records of the run (roadmap S24a); the engine sets them.
func (o *Output) WithResults(rows []record.Record, add func(record.Record) error) *Output {
	o.rows, o.result = rows, add
	return o
}

// WithRespond sets how Respond answers the waiting caller of an http trigger (S38c);
// the engine sets it when the run has one.
func (o *Output) WithRespond(respond action.Responder) *Output {
	o.respond = respond
	return o
}

// HasCaller reports whether a caller of an http trigger waits for the answer.
func (o *Output) HasCaller() bool { return o.respond != nil }

// Respond answers the waiting caller of an http trigger (http.response) and reports
// whether it was the first answer; false without a caller.
func (o *Output) Respond(r action.Response) bool {
	if o.respond == nil {
		return false
	}
	return o.respond(r)
}

// Rows returns the records passed to the run, e.g. by core.run-pipeline (rows); they
// must not be changed (emit copies).
func (o *Output) Rows() []record.Record { return o.rows }

// AddResult adds a record to the result of the run, e.g. for output.rows of
// core.run-pipeline. The record must not be changed afterwards.
func (o *Output) AddResult(r record.Record) error {
	if o.result == nil {
		return nil
	}
	return o.result(r)
}

// Fields of a record on an error link (roadmap S17a): the message, the field that
// caused it (empty if none) and the id of the transform.
const (
	ErrorField          = "_error"
	ErrorFieldField     = "_errorField"
	ErrorTransformField = "_errorTransform"
)

// WithErrorLinks sets where Fail sends records: the engine sets it for transforms with
// error links. Without, Fail returns the error and the pipeline stops.
func (o *Output) WithErrorLinks(send func(record.Record) error) *Output {
	o.fail = send
	return o
}

// Fail reports a record that could not be processed, e.g. a value that cannot be
// converted. err is the message as it would stop the pipeline ("record 3, field
// "amount": …"). With an error link, the record goes there with ErrorField,
// ErrorFieldField and ErrorTransformField set, and Fail returns nil (or the error of a
// cancelled run); without, Fail returns err, which the transform returns.
// The record must not be changed after Fail.
func (o *Output) Fail(r record.Record, field string, err error) error {
	if o.fail == nil {
		return err
	}
	r.Set(ErrorField, err.Error())
	r.Set(ErrorFieldField, field)
	r.Set(ErrorTransformField, o.id)
	return o.fail(r)
}

// WithValues sets what expressions see besides the record: vars, params, env and run
// (ADR 0006). The engine sets them once per run; they must not be changed.
func (o *Output) WithValues(values map[string]any) *Output {
	o.values = values
	return o
}

// Values returns what expressions see besides the record; see WithValues.
func (o *Output) Values() map[string]any { return o.values }

// TransformID returns the id of the transform in the pipeline.
func (o *Output) TransformID() string { return o.id }

// Emit passes a record to all outgoing links. It blocks while the following
// transforms are busy (backpressure) and returns an error when the pipeline is aborted.
// The record must not be changed after Emit.
func (o *Output) Emit(r record.Record) error { return o.emit(r) }

// Logf writes a formatted message to the run log.
func (o *Output) Logf(level slog.Level, format string, args ...any) {
	if o.log != nil {
		o.log(level, fmt.Sprintf(format, args...))
	}
}

// Categories group transforms in the editor's palette, in this order (KONZEPT, chapter 12.2).
const (
	CategoryInput     = "input"
	CategoryTransform = "transform"
	CategoryFlow      = "flow"
	CategoryExecution = "execution"
	CategoryCombine   = "combine"
	CategoryOutput    = "output"
)

// Categories lists all categories in palette order.
var Categories = []string{CategoryInput, CategoryTransform, CategoryFlow, CategoryExecution, CategoryCombine, CategoryOutput}

// Definition describes a transform type; like action.Definition (ADR 0014).
type Definition struct {
	// Type is the name used in pipeline files, e.g. "core.generate-rows".
	Type string
	// Category is one of Categories.
	Category    string
	Label       string
	Description string
	// Example is a short configuration ("with:", in YAML) shown in the help of the editor
	// (E14b); empty: none. The contract test checks that New accepts it.
	Example string
	// Config is the configuration struct that New decodes "with" into; nil for none.
	Config any
	// New creates a transform from its "with" configuration (variables resolved).
	New func(with map[string]any) (Transform, error)
	// RecordErrors is set for transforms that pass records they cannot process to an
	// error link (Output.Fail); an error link from another transform is never used.
	RecordErrors bool
	// Parallel is set for transforms that can run in several copies at once (copies: n)
	// because each record is handled on its own; not for inputs and outputs, which would
	// read or write twice.
	Parallel bool
	// DetectFields proposes the fields of an input transform from the first rows (at most
	// limit) of its input, for "Detect fields" in the editor (S46); nil if it cannot.
	// with is the configuration with variables resolved.
	DetectFields func(ctx context.Context, with map[string]any, limit int) (*Detection, error)
}

// Descriptor describes a transform type for the editor, the API and tools (ADR 0014).
type Descriptor struct {
	Type        string         `json:"type"`
	Kind        string         `json:"kind"` // always "transform"
	Category    string         `json:"category"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Schema      *schema.Schema `json:"schema"`
	// Example: see Definition.Example.
	Example string `json:"example,omitempty"`
	// RecordErrors: the transform can pass records to an error link (Definition.RecordErrors).
	RecordErrors bool `json:"recordErrors,omitempty"`
	// Parallel: the transform can run in several copies (Definition.Parallel).
	Parallel bool `json:"parallel,omitempty"`
	// DetectFields: the transform can propose its fields (Definition.DetectFields).
	DetectFields bool `json:"detectFields,omitempty"`
}

// Registry holds the available transform types.
type Registry struct {
	defs        map[string]Definition
	descriptors map[string]Descriptor
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{defs: map[string]Definition{}, descriptors: map[string]Descriptor{}}
}

// Register adds a transform type. It panics on invalid type names, duplicates and
// configuration structs without a valid schema, which are programming errors.
func (r *Registry) Register(d Definition) {
	if !model.ValidTypeName(d.Type) {
		panic(fmt.Sprintf("invalid transform type name %q (expected \"<namespace>.<name>\")", d.Type))
	}
	if _, dup := r.defs[d.Type]; dup {
		panic("transform type registered twice: " + d.Type)
	}
	if !slices.Contains(Categories, d.Category) {
		panic(fmt.Sprintf("transform type %s: unknown category %q", d.Type, d.Category))
	}
	s, err := schema.For(d.Config)
	if err != nil {
		panic(fmt.Sprintf("transform type %s: %v", d.Type, err))
	}
	r.defs[d.Type] = d
	r.descriptors[d.Type] = Descriptor{Type: d.Type, Kind: "transform", Category: d.Category, Label: d.Label, Description: d.Description, Schema: s, Example: d.Example, RecordErrors: d.RecordErrors, Parallel: d.Parallel,
		DetectFields: d.DetectFields != nil}
}

// Lookup returns the definition of a transform type.
func (r *Registry) Lookup(typ string) (Definition, bool) {
	d, ok := r.defs[typ]
	return d, ok
}

// Descriptor returns the descriptor of a transform type.
func (r *Registry) Descriptor(typ string) (Descriptor, bool) {
	d, ok := r.descriptors[typ]
	return d, ok
}

// Descriptors returns the descriptors of all transform types, sorted by type.
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

// DecodeConfig decodes a "with" configuration like action.DecodeConfig; numbers in
// fields of type any arrive as json.Number (record.Normalize makes them int64 or float64).
func DecodeConfig(with map[string]any, v any) error { return action.DecodeConfigNumbers(with, v) }
