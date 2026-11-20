package core

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

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
	// Rows is text, so that it can be a variable (${ROWS}); it is checked when the pipeline runs.
	Rows      string       `json:"rows" doc:"Number of records to create, e.g. 1000 or ${ROWS}" schema:"required,example=10"`
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
	for i, f := range t.Fields {
		if f.Name == "" {
			return nil, errors.New("fields: every field needs a name")
		}
		v, err := record.Normalize(f.Value)
		if err != nil {
			return nil, fmt.Errorf("fields[%d] (%s): %w", i, f.Name, err)
		}
		t.Fields[i].Value = v
	}
	return t, nil
}

// Init reports a closed schema: the row number (int) and the constant fields with the
// types of their values (ADR 0007).
func (t *generateRows) Init(*transform.InitContext) (record.Schema, error) {
	var s record.Schema
	if t.RowNumber != "" {
		s = s.With(t.RowNumber, record.Int)
	}
	for _, f := range t.Fields {
		s = s.With(f.Name, record.TypeOf(f.Value))
	}
	return s, nil
}

func (t *generateRows) Process(ctx context.Context, _ <-chan transform.Batch, out *transform.Output) error {
	rows, err := strconv.ParseInt(strings.TrimSpace(t.Rows), 10, 64)
	if err != nil || rows < 0 {
		return fmt.Errorf("rows: %q is not a whole number of 0 or more", t.Rows)
	}
	for n := int64(1); n <= rows; n++ {
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
