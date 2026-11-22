package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// core.map-values replaces the values of a field by a fixed table, e.g. status codes by
// their names. Values are compared as text (1 = "1"); a value that is not in the table
// gets the default, or stays as it is without one.
var mapValuesDefinition = transform.Definition{
	Parallel:    true,
	Type:        "core.map-values",
	Category:    transform.CategoryTransform,
	Label:       "Map Values",
	Description: "Replaces the values of a field by a table, e.g. codes by names",
	Example:     "field: status\ntarget: statusText\nvalues:\n  - { from: A, to: Active }\n  - { from: I, to: Inactive }\ndefault: Unknown\n",
	Config:      mapValues{},
	New:         newMapValues,
}

type mapValues struct {
	Field      string     `json:"field" doc:"Field whose values are replaced" schema:"required,format=runnel-field,example=status"`
	Target     string     `json:"target" doc:"Field for the result; empty: the field itself"`
	Values     []mapEntry `json:"values" doc:"The table: a value and what it becomes" schema:"required"`
	Default    any        `json:"default" doc:"Result for values that are not in the table, also for empty ones; missing: the value stays as it is"`
	IgnoreCase bool       `json:"ignoreCase" doc:"Compare text regardless of upper and lower case" schema:"default=false"`

	table map[string]any
}

type mapEntry struct {
	From string `json:"from" doc:"Value to replace, compared as text; empty: an empty value" schema:"required,example=A"`
	To   any    `json:"to" doc:"What it becomes: text, number or true/false; missing: an empty value"`
}

func newMapValues(with map[string]any) (transform.Transform, error) {
	t := &mapValues{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	if t.Field == "" {
		return nil, errors.New("field: name the field whose values are replaced")
	}
	if len(t.Values) == 0 {
		return nil, errors.New("values: list at least one value")
	}
	t.table = make(map[string]any, len(t.Values))
	for i, e := range t.Values {
		key := t.key(e.From)
		if _, dup := t.table[key]; dup {
			return nil, fmt.Errorf("values[%d]: %q is listed twice", i, e.From)
		}
		v, err := record.Normalize(e.To)
		if err != nil {
			return nil, fmt.Errorf("values[%d] (%s): %w", i, e.From, err)
		}
		t.table[key] = v
	}
	if t.Default != nil {
		v, err := record.Normalize(t.Default)
		if err != nil {
			return nil, fmt.Errorf("default: %w", err)
		}
		t.Default = v
	}
	return t, nil
}

func (t *mapValues) key(s string) string {
	if t.IgnoreCase {
		return strings.ToLower(s)
	}
	return s
}

func (t *mapValues) target() string {
	if t.Target != "" {
		return t.Target
	}
	return t.Field
}

// Init checks the field and gives the result the type of the values of the table, if
// they have one type and every value is replaced (with a default); otherwise it is
// known only when the pipeline runs.
func (t *mapValues) Init(ic *transform.InitContext) (record.Schema, error) {
	ic.Use(t.Field, "field")
	typ := record.Unknown
	if t.Default != nil {
		typ = record.TypeOf(t.Default)
		for _, v := range t.table {
			if record.TypeOf(v) != typ {
				typ = record.Unknown
				break
			}
		}
	}
	return ic.In.With(t.target(), typ), nil
}

func (t *mapValues) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	target := t.target()
	for b := range in {
		for _, r := range b {
			v, _ := r.Get(t.Field)
			if to, ok := t.table[t.key(expr.Format(v))]; ok {
				r.Set(target, to)
			} else if t.Default != nil {
				r.Set(target, t.Default)
			} else if target != t.Field {
				r.Set(target, v)
			}
			if err := out.Emit(r); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}
