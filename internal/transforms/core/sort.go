package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// core.sort sorts all records by fields. It keeps the records in memory until its
// input has ended, so memory grows with the number of records (external sorting for
// large amounts of data follows later, KONZEPT 12.2).
var sortDefinition = transform.Definition{
	Type:         "core.sort",
	Category:     transform.CategoryCombine,
	Label:        "Sort",
	Description:  "Sorts the records by fields; keeps all records in memory",
	Example:      "fields:\n  - { name: total, descending: true }\n  - { name: name, ignoreCase: true }\n",
	Config:       sortRecords{},
	New:          newSort,
	RecordErrors: true,
	Reference: action.Reference{
		Records: "Passes all records on, sorted and stable, once its input has ended; empty values come last.",
		Errors:  "A record whose value cannot be sorted together with the values before (another kind, e.g. text among numbers) goes to the error link; without one, the pipeline stops.",
		Limits:  []string{"Holds all records in memory until its input has ended."},
	},
}

type sortRecords struct {
	Fields []sortField `json:"fields" doc:"Fields to sort by, the first one first" schema:"required"`
}

type sortField struct {
	Name       string `json:"name" doc:"Name of the field" schema:"required,format=runnel-field,example=country"`
	Descending bool   `json:"descending" doc:"Largest value first" schema:"default=false"`
	IgnoreCase bool   `json:"ignoreCase" doc:"Compare text regardless of upper and lower case" schema:"default=false"`
}

func newSort(with map[string]any) (transform.Transform, error) {
	t := &sortRecords{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	if len(t.Fields) == 0 {
		return nil, errors.New("fields: list at least one field to sort by")
	}
	return t, nil
}

// Init checks the fields and passes the schema on: only the order changes.
func (t *sortRecords) Init(ic *transform.InitContext) (record.Schema, error) {
	for i, f := range t.Fields {
		ic.Use(f.Name, fmt.Sprintf("fields[%d].name", i))
	}
	return ic.In, nil
}

func (t *sortRecords) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	// All values of a field must be of one kind (numbers, text …): a record with
	// another kind goes to the error link, so that the comparison is always defined.
	kinds := make([]record.Kind, len(t.Fields))
	var records []record.Record
	var n int64
	for b := range in {
	records:
		for _, r := range b {
			n++
			for i, f := range t.Fields {
				v, _ := r.Get(f.Name)
				k := record.KindOf(v)
				switch {
				case k == record.KindNone:
				case k == record.Incomparable:
					if err := out.Fail(r, f.Name, fmt.Errorf("record %d, field %q: a %s value cannot be sorted", n, f.Name, record.TypeOf(v))); err != nil {
						return err
					}
					continue records
				case kinds[i] == record.KindNone:
					kinds[i] = k
				case kinds[i] != k:
					if err := out.Fail(r, f.Name, fmt.Errorf("record %d, field %q: %s cannot be sorted together with %s values before", n, f.Name, record.TypeOf(v), kinds[i])); err != nil {
						return err
					}
					continue records
				}
			}
			records = append(records, r)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	slices.SortStableFunc(records, t.compare)
	out.Logf(slog.LevelDebug, "sorted %d records", len(records))
	for _, r := range records {
		if err := out.Emit(r); err != nil {
			return err
		}
	}
	return nil
}

// compare orders two records by the fields; empty values (null) come last, also when
// sorting in descending order.
func (t *sortRecords) compare(a, b record.Record) int {
	for _, f := range t.Fields {
		x, _ := a.Get(f.Name)
		y, _ := b.Get(f.Name)
		switch {
		case x == nil && y == nil:
			continue
		case x == nil:
			return 1
		case y == nil:
			return -1
		}
		if f.IgnoreCase {
			if s, ok := x.(string); ok {
				x = strings.ToLower(s)
			}
			if s, ok := y.(string); ok {
				y = strings.ToLower(s)
			}
		}
		c := record.Compare(x, y)
		if f.Descending {
			c = -c
		}
		if c != 0 {
			return c
		}
	}
	return 0
}
