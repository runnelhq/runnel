// Package csv contains the transforms of the namespace "csv" (roadmap S15): csv.input
// reads a CSV file, csv.output writes one. Without a field definition all values are
// text; with fields: they are converted with type and format (ADR 0007). Each transform
// lives in a file of its own; this file holds what both share.
package csv

import (
	"fmt"

	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/record"
)

// field is a column of a field definition (fields:).
type field struct {
	Name string `json:"name" doc:"Name of the field" schema:"required,example=amount"`
	From string `json:"from" doc:"Column in the header; empty: the column named like the field" schema:"example=Betrag"`
	Type string `json:"type" doc:"Type of the value; empty: text" schema:"enum=string|int|decimal|float|bool|date|datetime|json"`
	// Format is a number pattern like #.##0,00 or a date pattern like dd.MM.yyyy.
	Format string `json:"format" doc:"Number pattern like #.##0,00 or date pattern like dd.MM.yyyy; empty: 1234.5 and ISO dates" schema:"example=dd.MM.yyyy"`
}

// codec converts the text of a column (internal/format).
type codec = format.Codec

func newCodec(f field) (codec, error) {
	c, err := format.New(record.Type(f.Type), f.Format)
	if err != nil {
		return c, fmt.Errorf("field %q: %w", f.Name, err)
	}
	return c, nil
}

// textCodec reads columns without field definition: text as it is.
var textCodec, _ = format.New(record.String, "")
