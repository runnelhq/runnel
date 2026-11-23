package transform

import (
	"fmt"
	"strings"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/record"
)

// Initializer is implemented by transforms that derive the schema of their output
// (ADR 0007). The engine calls Init once before the run, in the order of the links.
// Transforms without it produce an open schema about which nothing is known.
type Initializer interface {
	// Init receives the schema of the incoming records – merged from all incoming links;
	// empty and closed for a transform without input – and returns the schema of the
	// records it emits. Fields the transform reads are reported with ic.Use.
	Init(ic *InitContext) (record.Schema, error)
}

// InitContext is what Init needs: the incoming schema and a place for problems.
type InitContext struct {
	In record.Schema
	// Targets are the ids of the transforms the data links of the transform lead to,
	// in the order of the links (for Output.EmitTo).
	Targets []string
	// Strict is set in the strict mode of a pipeline (ADR 0007): a field that is not
	// known is an error also in an open schema.
	Strict bool
	// Info is the schema of the records on the info link (S23b); nil without one.
	Info *record.Schema
	// Inputs are the schemas of the incoming data and error links by the id of the
	// transform they come from (S23c), for transforms with NamedInputs.
	Inputs   map[string]record.Schema
	problems []Problem
}

// Problem is an error or warning found by Init, located in the configuration.
type Problem struct {
	// Path within "with", e.g. "fields[1]"; empty for the configuration as a whole.
	Path    string
	Message string
	Warning bool
}

// NewInitContext creates the context for Init with the incoming schema.
func NewInitContext(in record.Schema) *InitContext {
	return &InitContext{In: in}
}

// Use reports that the transform reads a field, configured at path (within "with").
// A field missing from a closed schema is an error; a field not known in an open
// schema is a warning (a typo?), both with a suggestion of a similar name (ADR 0007).
// An open schema without any known field gives no warning: there is nothing to
// compare with. Fields starting with "_" are metadata and not checked.
func (ic *InitContext) Use(name, path string) {
	if strings.HasPrefix(name, "_") {
		return
	}
	if _, ok := ic.In.Field(name); ok {
		return
	}
	names := ic.In.Names()
	if ic.In.Open && len(names) == 0 && !ic.Strict {
		return
	}
	hint := model.Suggest(name, names)
	if !ic.In.Open || ic.Strict {
		known := "none"
		if len(names) > 0 {
			known = strings.Join(names, ", ")
		}
		ic.problems = append(ic.problems, Problem{Path: path,
			Message: fmt.Sprintf("field %q does not exist in the incoming records%s (fields: %s)", name, hint, known)})
		return
	}
	ic.problems = append(ic.problems, Problem{Path: path, Warning: true,
		Message: fmt.Sprintf("field %q is not known in the incoming records%s; it is checked when the pipeline runs", name, hint)})
}

// UseInfo reports that the transform reads a field of the records on its info link,
// configured at path; like Use, with a warning while there is no info link.
func (ic *InitContext) UseInfo(name, path string) {
	if ic.Info == nil {
		ic.problems = append(ic.problems, Problem{Path: path, Warning: true,
			Message: fmt.Sprintf("field %q of the lookup data is not known: there is no info link yet", name)})
		return
	}
	in := ic.In
	ic.In = *ic.Info
	defer func() { ic.In = in }()
	n := len(ic.problems)
	ic.Use(name, path)
	for i := n; i < len(ic.problems); i++ {
		ic.problems[i].Message = strings.Replace(ic.problems[i].Message, "in the incoming records", "in the lookup data", 1)
	}
}

// Error reports an error in the configuration at path (within "with").
func (ic *InitContext) Error(path, message string) {
	ic.problems = append(ic.problems, Problem{Path: path, Message: message})
}

// Warn reports a warning about the configuration at path (within "with"; empty for the
// configuration as a whole).
func (ic *InitContext) Warn(path, message string) {
	ic.problems = append(ic.problems, Problem{Path: path, Message: message, Warning: true})
}

// Problems returns the problems reported so far.
func (ic *InitContext) Problems() []Problem { return ic.problems }
