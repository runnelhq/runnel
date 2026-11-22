package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// core.calculate sets fields to the result of an expression for every record, e.g. a
// total from price and quantity. Fields are calculated in their order, so a later field
// can use an earlier one.
var calculateDefinition = transform.Definition{
	Parallel:     true,
	Type:         "core.calculate",
	RecordErrors: true,
	Category:     transform.CategoryTransform,
	Label:        "Calculate",
	Description:  "Sets fields to the result of an expression for every record",
	Example:      "fields:\n  - { name: total, value: \"{{ record.price * record.quantity }}\" }\n  - { name: label, value: \"{{ upper(record.name) }}: {{ record.total }}\" }\n",
	Config:       calculate{},
	New:          newCalculate,
}

type calculate struct {
	Fields []calculateField `json:"fields" doc:"Fields to calculate, in this order: a later field can use an earlier one" schema:"required"`

	exprs []*transform.Expression
}

type calculateField struct {
	Name  string `json:"name" doc:"Name of the field; an existing field of the same name gets the new value" schema:"required,example=total"`
	Value string `json:"value" doc:"Expression evaluated for every record, e.g. {{ record.price * record.quantity }}; text around {{ … }} makes the result a text" schema:"required,format=runnel-text,example={{ record.price * record.quantity }}"`
}

func newCalculate(with map[string]any) (transform.Transform, error) {
	t := &calculate{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	if len(t.Fields) == 0 {
		return nil, errors.New("fields: list at least one field")
	}
	t.exprs = make([]*transform.Expression, len(t.Fields))
	for i, f := range t.Fields {
		if f.Name == "" {
			return nil, fmt.Errorf("fields[%d]: every field needs a name", i)
		}
		e, err := transform.CompileExpression(f.Value)
		if err != nil {
			return nil, fmt.Errorf("fields[%d] (%s): %w", i, f.Name, err)
		}
		t.exprs[i] = e
	}
	return t, nil
}

// Init checks the fields the expressions read (except those calculated before) and adds
// the calculated fields; their type is known only when the pipeline runs.
func (t *calculate) Init(ic *transform.InitContext) (record.Schema, error) {
	s := ic.In
	calculated := map[string]bool{}
	for i, f := range t.Fields {
		for _, name := range t.exprs[i].Fields() {
			if !calculated[name] {
				ic.Use(name, fmt.Sprintf("fields[%d].value", i))
			}
		}
		calculated[f.Name] = true
		s = s.With(f.Name, record.Unknown)
	}
	return s, nil
}

// previous is the value a field had before it was calculated, to restore the record when
// a later field fails: the error link gets the record as it came in.
type previous struct {
	value  any
	exists bool
}

func (t *calculate) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	var n, failed int64
	before := make([]previous, len(t.Fields))
	for b := range in {
	records:
		for _, r := range b {
			n++
			for i, f := range t.Fields {
				v, err := t.exprs[i].Value(out, r)
				if err == nil {
					v, err = record.Normalize(v)
				}
				if err != nil {
					for j := i - 1; j >= 0; j-- {
						if before[j].exists {
							r.Set(t.Fields[j].Name, before[j].value)
						} else {
							r.Delete(t.Fields[j].Name)
						}
					}
					failed++
					if err := out.Fail(r, f.Name, fmt.Errorf("record %d, field %q: %w", n, f.Name, err)); err != nil {
						return err
					}
					continue records
				}
				before[i].value, before[i].exists = r.Get(f.Name)
				r.Set(f.Name, v)
			}
			if err := out.Emit(r); err != nil {
				return err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if failed > 0 {
		out.Logf(slog.LevelInfo, "calculated %d of %d records, %d with errors", n-failed, n, failed)
	}
	return nil
}
