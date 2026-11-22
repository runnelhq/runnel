package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// core.constant adds fields with the same value to every record. Values may contain
// ${…} and {{ … }}; expressions are evaluated once when the pipeline starts.
var constantDefinition = transform.Definition{
	Parallel:    true,
	Type:        "core.constant",
	Category:    transform.CategoryTransform,
	Label:       "Add Constants",
	Description: "Adds fields with the same value to every record",
	Example:     "fields:\n  - { name: source, value: runnel }\n  - { name: batch, value: \"{{ params.BATCH }}\" }\n",
	Config:      constant{},
	New:         newConstant,
}

type constant struct {
	Fields []constantField `json:"fields" doc:"Fields to add; an existing field of the same name gets the value" schema:"required"`

	exprs []*expr.Template // per field; nil for a plain value
}

type constantField struct {
	Name  string `json:"name" doc:"Name of the field" schema:"required,example=source"`
	Value any    `json:"value" doc:"Value of the field: text, number, true/false, or an expression evaluated once per run, e.g. {{ now() }}"`
}

func newConstant(with map[string]any) (transform.Transform, error) {
	t := &constant{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	if len(t.Fields) == 0 {
		return nil, errors.New("fields: list at least one field")
	}
	t.exprs = make([]*expr.Template, len(t.Fields))
	for i, f := range t.Fields {
		if f.Name == "" {
			return nil, fmt.Errorf("fields[%d]: every field needs a name", i)
		}
		if s, ok := f.Value.(string); ok && expr.Contains(s) {
			// The values of the run (params …) are known when it starts; before, only checked.
			if hasVariable(s) {
				if err := expr.Check(s, model.PipelineRoots); err != nil {
					return nil, fmt.Errorf("fields[%d] (%s): %w", i, f.Name, err)
				}
				continue
			}
			tpl, err := expr.Compile(s, model.PipelineRoots)
			if err != nil {
				return nil, fmt.Errorf("fields[%d] (%s): %w", i, f.Name, err)
			}
			t.exprs[i] = tpl
			continue
		}
		v, err := record.Normalize(f.Value)
		if err != nil {
			return nil, fmt.Errorf("fields[%d] (%s): %w", i, f.Name, err)
		}
		t.Fields[i].Value = v
	}
	return t, nil
}

// Init adds the fields to the incoming schema: with the type of their value, unknown
// for expressions.
func (t *constant) Init(ic *transform.InitContext) (record.Schema, error) {
	s := ic.In
	for i, f := range t.Fields {
		typ := record.TypeOf(f.Value)
		if t.exprs[i] != nil || isExpression(f.Value) {
			typ = record.Unknown
		}
		s = s.With(f.Name, typ)
	}
	return s, nil
}

func isExpression(v any) bool {
	s, ok := v.(string)
	return ok && expr.Contains(s)
}

func (t *constant) Process(_ context.Context, in <-chan transform.Batch, out *transform.Output) error {
	values := make([]any, len(t.Fields))
	for i, f := range t.Fields {
		values[i] = f.Value
		if t.exprs[i] == nil {
			if isExpression(f.Value) {
				return fmt.Errorf("fields[%d] (%s): the value contains a variable ${…} that was not resolved", i, f.Name)
			}
			continue
		}
		v, err := t.exprs[i].Eval(out.Values())
		if err == nil {
			v, err = record.Normalize(v)
		}
		if err != nil {
			return fmt.Errorf("fields[%d] (%s): %w", i, f.Name, err)
		}
		values[i] = v
	}
	for b := range in {
		for _, r := range b {
			for i, f := range t.Fields {
				r.Set(f.Name, values[i])
			}
			if err := out.Emit(r); err != nil {
				return err
			}
		}
	}
	return nil
}

// hasVariable reports whether a text still contains ${…}: runnel validate checks the
// configuration before variables are resolved.
func hasVariable(s string) bool { return strings.Contains(s, "${") }
