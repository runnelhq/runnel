package core

import (
	"context"
	"errors"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// core.generate-rows creates a number of records with constant fields, e.g. for tests
// or to start a pipeline with one record.
var generateRowsDefinition = transform.Definition{
	Type:        "core.generate-rows",
	Category:    transform.CategoryInput,
	Label:       "Generate Rows",
	Description: "Creates records with constant fields",
	Config:      generateRows{},
	New:         newGenerateRows,
}

type generateRows struct {
	Rows      int64        `json:"rows" doc:"Number of records to create" schema:"required,example=10"`
	Fields    []constField `json:"fields" doc:"Fields of every record, with a constant value"`
	RowNumber string       `json:"rowNumberField" doc:"Name of a field for the number of the record, starting at 1; empty: none"`
}

type constField struct {
	Name  string `json:"name" doc:"Name of the field" schema:"required,example=status"`
	Value any    `json:"value" doc:"Value of the field"`
}

func newGenerateRows(with map[string]any) (transform.Transform, error) {
	t := &generateRows{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	if t.Rows < 0 {
		return nil, errors.New("rows must not be negative")
	}
	for _, f := range t.Fields {
		if f.Name == "" {
			return nil, errors.New("fields: every field needs a name")
		}
	}
	return t, nil
}

func (t *generateRows) Process(ctx context.Context, _ <-chan transform.Batch, out *transform.Output) error {
	for n := int64(1); n <= t.Rows; n++ {
		var r record.Record
		if t.RowNumber != "" {
			r.Set(t.RowNumber, n)
		}
		for _, f := range t.Fields {
			r.Set(f.Name, f.Value)
		}
		if err := out.Emit(r); err != nil {
			return err
		}
	}
	return ctx.Err()
}
