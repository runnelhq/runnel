package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

func TestConvert(t *testing.T) {
	tr := transformtest.New(t, convertDefinition, map[string]any{"fields": []any{
		map[string]any{"name": "amount", "type": "decimal", "format": "#.##0,00"},
		map[string]any{"name": "day", "type": "date", "format": "dd.MM.yyyy"},
		map[string]any{"name": "qty", "type": "int"},
		map[string]any{"name": "total", "type": "string", "format": "#,##0.00"},
		map[string]any{"name": "missing", "type": "int"},
	}})
	in := record.New(
		record.Field{Name: "amount", Value: "1.234,50"},
		record.Field{Name: "day", Value: "20.11.2026"},
		record.Field{Name: "qty", Value: ""},
		record.Field{Name: "total", Value: decimal.RequireFromString("1234567.891")},
	)
	out, _, err := transformtest.Run(context.Background(), tr, in)
	if err != nil || len(out) != 1 {
		t.Fatalf("%d records, %v", len(out), err)
	}
	// A field that is not in the record stays missing; an empty text becomes null.
	if got, want := Format(out[0]), `{amount: 1234.5, day: 2026-11-20, qty: null, total: "1,234,567.89"}`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if v, _ := out[0].Get("day"); v != (record.Date{Year: 2026, Month: time.November, Day: 20}) {
		t.Errorf("day %#v", v)
	}
}

func TestConvertErrors(t *testing.T) {
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"fields": []any{}}, "fields: list at least one field"},
		{map[string]any{"fields": []any{map[string]any{"name": "a", "type": "money"}}}, `fields[0] (a): invalid type "money"`},
		{map[string]any{"fields": []any{map[string]any{"name": "a", "type": "bool", "format": "0"}}}, "fields[0] (a): a format is only possible for numbers and dates"},
		{map[string]any{"fields": []any{map[string]any{"name": "a", "type": "decimal", "format": "#.##0,00.0"}}}, "fields[0] (a): invalid number format"},
		{map[string]any{"fields": []any{map[string]any{"name": "a"}}}, `missing required field "fields[0].type"`},
	} {
		if _, err := convertDefinition.New(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
	tr := transformtest.New(t, convertDefinition, map[string]any{"fields": []any{map[string]any{"name": "amount", "type": "int"}}})
	_, _, err := transformtest.Run(context.Background(), tr,
		record.New(record.Field{Name: "amount", Value: "12"}), record.New(record.Field{Name: "amount", Value: "12,5"}))
	if err == nil || err.Error() != `record 2, field "amount": "12,5" is not a whole number (expected e.g. 1234.5; set a format like #.##0,00 for other separators)` {
		t.Errorf("conversion: %v", err)
	}
}

func TestConvertSchema(t *testing.T) {
	tr := transformtest.New(t, convertDefinition, map[string]any{"fields": []any{
		map[string]any{"name": "amount", "type": "decimal"}, map[string]any{"name": "dy", "type": "date"}}})
	ic := transform.NewInitContext(record.Schema{}.With("amount", record.String).With("day", record.String))
	s, err := tr.(transform.Initializer).Init(ic)
	if err != nil {
		t.Fatal(err)
	}
	if s.Fields[0].Type != record.Decimal || s.Fields[1].Type != record.String || len(s.Fields) != 2 {
		t.Errorf("schema %+v", s)
	}
	if p := ic.Problems(); len(p) != 1 || p[0].Path != "fields[1].name" || !strings.Contains(p[0].Message, `did you mean "day"?`) {
		t.Errorf("problems %+v", p)
	}
	// In an open schema without known fields, nothing is added: no warnings for other fields later.
	s, _ = tr.(transform.Initializer).Init(transform.NewInitContext(record.OpenSchema()))
	if len(s.Fields) != 0 || !s.Open {
		t.Errorf("open: %+v", s)
	}
}

func TestConvertErrorLink(t *testing.T) {
	tr := transformtest.New(t, convertDefinition, map[string]any{"fields": []any{
		map[string]any{"name": "id", "type": "int"},
		map[string]any{"name": "amount", "type": "decimal", "format": "#.##0,00"},
	}})
	in := func(id, amount string) record.Record {
		return record.New(record.Field{Name: "id", Value: id}, record.Field{Name: "amount", Value: amount})
	}
	out, failed, _, err := transformtest.RunWithErrorLink(context.Background(), tr, in("1", "1,50"), in("2", "viel"), in("3", "3,00"))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || Format(out[1]) != "{id: 3, amount: 3}" {
		t.Errorf("out %v", out)
	}
	// The record goes on unchanged (id is still text), with the error fields.
	want := `{id: "2", amount: "viel", _error: "record 2, field \"amount\": \"viel\" is not a number", _errorField: "amount", _errorTransform: "t"}`
	if len(failed) != 1 || Format(failed[0]) != want {
		t.Errorf("failed %v\nwant %s", failed, want)
	}
}
