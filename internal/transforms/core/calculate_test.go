package core

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

func item(name string, price string, qty int64) record.Record {
	return record.New(record.Field{Name: "name", Value: name}, record.Field{Name: "price", Value: decimal.RequireFromString(price)},
		record.Field{Name: "qty", Value: qty})
}

// TestCalculate: exact decimals, a later field uses an earlier one, text around {{ }},
// an existing field is replaced in place, the values of the run.
func TestCalculate(t *testing.T) {
	tr := transformtest.New(t, calculateDefinition, map[string]any{"fields": []any{
		map[string]any{"name": "total", "value": "{{ record.price * record.qty }}"},
		map[string]any{"name": "label", "value": "{{ upper(record.name) }}: {{ record.total }}"},
		map[string]any{"name": "name", "value": "{{ params.PREFIX + record.name }}"},
	}})
	values := map[string]any{"params": map[string]any{"PREFIX": "#"}, "vars": map[string]any{}, "env": map[string]any{}, "run": map[string]any{}}
	out, _, err := transformtest.RunWithValues(context.Background(), tr, values, item("pen", "1.10", 3), item("ink", "0.99", 0))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range out {
		got = append(got, fmt.Sprint(r.Fields()))
	}
	want := []string{
		"[{name #pen} {price 1.1} {qty 3} {total 3.3} {label PEN: 3.3}]",
		"[{name #ink} {price 0.99} {qty 0} {total 0} {label INK: 0}]",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if v, _ := out[0].Get("total"); fmt.Sprintf("%T", v) != "decimal.Decimal" {
		t.Errorf("total is %T, want an exact decimal", v)
	}
}

// TestCalculateErrorLink: a record whose expression fails goes to the error link as it
// came in, without the fields calculated before.
func TestCalculateErrorLink(t *testing.T) {
	tr := transformtest.New(t, calculateDefinition, map[string]any{"fields": []any{
		map[string]any{"name": "total", "value": "{{ record.price * record.qty }}"},
		map[string]any{"name": "qty", "value": "{{ record.total / record.price }}"},
		map[string]any{"name": "big", "value": "{{ record.name > 1 }}"},
	}})
	out, failed, logs, err := transformtest.RunWithErrorLink(context.Background(), tr, item("pen", "1.10", 3))
	if err != nil || len(out) != 0 || len(failed) != 1 {
		t.Fatalf("out %v, failed %v, %v", out, failed, err)
	}
	r := failed[0]
	if _, ok := r.Get("total"); ok {
		t.Errorf("total is still set: %v", r.Fields())
	}
	if v, _ := r.Get("qty"); v != int64(3) {
		t.Errorf("qty = %v, want the incoming 3", v)
	}
	if v, _ := r.Get("_error"); !strings.Contains(fmt.Sprint(v), `record 1, field "big": expression {{ record.name > 1 }}`) {
		t.Errorf("_error %v", v)
	}
	if len(logs) != 1 || logs[0].Msg != "calculated 0 of 1 records, 1 with errors" {
		t.Errorf("log %v", logs)
	}
	// Without an error link the pipeline stops.
	if _, _, err := transformtest.Run(context.Background(), tr, item("pen", "1.10", 3)); err == nil {
		t.Error("no error without an error link")
	}
}

func TestCalculateSchema(t *testing.T) {
	tr := transformtest.New(t, calculateDefinition, map[string]any{"fields": []any{
		map[string]any{"name": "total", "value": "{{ record.prise * record.qty }}"},
		map[string]any{"name": "double", "value": "{{ record.total * 2 }}"},
	}})
	in := record.Schema{}.With("price", record.Decimal).With("qty", record.Int)
	ic := transform.NewInitContext(in)
	s, err := tr.(transform.Initializer).Init(ic)
	if err != nil || strings.Join(s.Names(), ",") != "price,qty,total,double" {
		t.Errorf("schema %v %v", s.Names(), err)
	}
	// total is calculated before double: only the typo is reported.
	if p := ic.Problems(); len(p) != 1 || p[0].Path != "fields[0].value" || !strings.Contains(p[0].Message, `"prise" does not exist`) {
		t.Errorf("problems %+v", p)
	}
}

func TestCalculateConfig(t *testing.T) {
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"fields": []any{}}, "list at least one field"},
		{map[string]any{"fields": []any{map[string]any{"name": "", "value": "{{ 1 }}"}}}, "every field needs a name"},
		{map[string]any{"fields": []any{map[string]any{"name": "x", "value": "{{ record.a + }}"}}}, "fields[0] (x)"},
	} {
		if _, err := newCalculate(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: err = %v, want %q", tt.with, err, tt.want)
		}
	}
}

func BenchmarkCalculate(b *testing.B) {
	tr, err := newCalculate(map[string]any{"fields": []any{map[string]any{"name": "total", "value": "{{ record.price * record.qty }}"}}})
	if err != nil {
		b.Fatal(err)
	}
	in := make([]record.Record, 1000)
	b.ReportAllocs()
	for b.Loop() {
		for i := range in {
			in[i] = item("pen", "1.10", int64(i))
		}
		if _, _, err := transformtest.Run(context.Background(), tr, in...); err != nil {
			b.Fatal(err)
		}
	}
}
