package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// core.convert converts fields to another type, e.g. text in German format to decimal.
var convertDefinition = transform.Definition{
	Type:        "core.convert",
	Category:    transform.CategoryTransform,
	Label:       "Convert Types",
	Description: "Converts fields to another type, with a number or date format",
	Config:      convert{},
	New:         newConvert,
}

type convert struct {
	Fields []convertField `json:"fields" doc:"Fields to convert" schema:"required"`

	codecs []format.Codec
}

type convertField struct {
	Name   string `json:"name" doc:"Name of the field" schema:"required,example=amount"`
	Type   string `json:"type" doc:"Type to convert to" schema:"required,enum=string|int|decimal|float|bool|date|datetime|json"`
	Format string `json:"format" doc:"Number pattern like #.##0,00 or date pattern like dd.MM.yyyy, to read text or to write text; empty: 1234.5 and ISO dates" schema:"example=dd.MM.yyyy"`
}

func newConvert(with map[string]any) (transform.Transform, error) {
	t := &convert{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	if len(t.Fields) == 0 {
		return nil, errors.New("fields: list at least one field")
	}
	for i, f := range t.Fields {
		if f.Name == "" {
			return nil, fmt.Errorf("fields[%d]: every field needs a name", i)
		}
		var c format.Codec
		var err error
		if record.Type(f.Type) == record.String {
			c, err = format.ToText(f.Format)
		} else {
			c, err = format.New(record.Type(f.Type), f.Format)
		}
		if f.Type == "" {
			err = fmt.Errorf("a type is required (%s)", format.TypeNames())
		}
		if err != nil {
			return nil, fmt.Errorf("fields[%d] (%s): %w", i, f.Name, err)
		}
		t.codecs = append(t.codecs, c)
	}
	return t, nil
}

// Init checks the fields and changes the types of the known ones. In an open schema, a
// field that is not known is not added: the schema would then know a few fields and
// report all others as possible typos (ADR 0007).
func (t *convert) Init(ic *transform.InitContext) (record.Schema, error) {
	s := ic.In
	for i, f := range t.Fields {
		ic.Use(f.Name, fmt.Sprintf("fields[%d].name", i))
		if _, ok := s.Field(f.Name); ok {
			s = s.With(f.Name, t.codecs[i].Type())
		}
	}
	return s, nil
}

func (t *convert) Process(_ context.Context, in <-chan transform.Batch, out *transform.Output) error {
	var n int64
	for b := range in {
		for _, r := range b {
			n++
			for i, f := range t.Fields {
				v, ok := r.Get(f.Name)
				if !ok {
					continue // not in this record: nothing to convert
				}
				c, err := t.codecs[i].Convert(v)
				if err != nil {
					return fmt.Errorf("record %d, field %q: %w", n, f.Name, err)
				}
				r.Set(f.Name, c)
			}
			if err := out.Emit(r); err != nil {
				return err
			}
		}
	}
	return nil
}
