package core

import (
	"context"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

func sales() []record.Record {
	s := func(country, city string, amount any, qty int64) record.Record {
		return record.New(record.Field{Name: "country", Value: country}, record.Field{Name: "city", Value: city},
			record.Field{Name: "amount", Value: amount}, record.Field{Name: "qty", Value: qty})
	}
	d := decimal.RequireFromString
	return []record.Record{
		s("DE", "Köln", d("10.50"), 1),
		s("AT", "Wien", d("5"), 2),
		s("DE", "Bonn", nil, 3),
		s("DE", "Köln", d("0.25"), 4),
		s("FR", "Paris", d("1.5"), 5),
	}
}

func runGroup(t *testing.T, with map[string]any, in ...record.Record) []string {
	t.Helper()
	tr := transformtest.New(t, groupByDefinition, with)
	out, _, err := transformtest.Run(context.Background(), tr, in...)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, r := range out {
		lines = append(lines, Format(r))
	}
	return lines
}

func TestGroupBy(t *testing.T) {
	got := runGroup(t, map[string]any{
		"groupBy": []any{"country"},
		"aggregates": []any{
			map[string]any{"name": "records", "function": "count"},
			map[string]any{"name": "amounts", "function": "count", "field": "amount"},
			map[string]any{"name": "total", "function": "sum", "field": "amount"},
			map[string]any{"name": "average", "function": "avg", "field": "amount"},
			map[string]any{"name": "pieces", "function": "sum", "field": "qty"},
			map[string]any{"name": "smallest", "function": "min", "field": "amount"},
			map[string]any{"name": "largest", "function": "max", "field": "qty"},
			map[string]any{"name": "firstCity", "function": "first", "field": "city"},
			map[string]any{"name": "lastCity", "function": "last", "field": "city"},
			map[string]any{"name": "cities", "function": "countDistinct", "field": "city"},
			map[string]any{"name": "list", "function": "concat", "field": "city", "separator": " / "},
		},
	}, sales()...)
	// Groups in the order of first appearance; empty values are skipped (SQL).
	want := []string{
		`{country: "DE", records: 3, amounts: 2, total: 10.75, average: 5.375, pieces: 8, smallest: 0.25, largest: 4, firstCity: "Köln", lastCity: "Köln", cities: 2, list: "Köln / Bonn / Köln"}`,
		`{country: "AT", records: 1, amounts: 1, total: 5, average: 5, pieces: 2, smallest: 5, largest: 2, firstCity: "Wien", lastCity: "Wien", cities: 1, list: "Wien"}`,
		`{country: "FR", records: 1, amounts: 1, total: 1.5, average: 1.5, pieces: 5, smallest: 1.5, largest: 5, firstCity: "Paris", lastCity: "Paris", cities: 1, list: "Paris"}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestGroupByWithoutGroups(t *testing.T) {
	with := map[string]any{"aggregates": []any{
		map[string]any{"name": "n", "function": "count"},
		map[string]any{"name": "total", "function": "sum", "field": "qty"},
		map[string]any{"name": "avg", "function": "avg", "field": "qty"},
	}}
	if got := runGroup(t, with, sales()...); len(got) != 1 || got[0] != "{n: 5, total: 15, avg: 3}" {
		t.Errorf("all: %v", got)
	}
	// Without input: one record, count 0 and sums empty, as in SQL.
	if got := runGroup(t, with); len(got) != 1 || got[0] != "{n: 0, total: null, avg: null}" {
		t.Errorf("no input: %v", got)
	}
	// With group fields and without input: no records.
	if got := runGroup(t, map[string]any{"groupBy": []any{"country"}}); len(got) != 0 {
		t.Errorf("no input, groups: %v", got)
	}
	// Numbers group across types: 1 and 1.0 are one group; null is a group of its own.
	in := []record.Record{
		record.New(record.Field{Name: "k", Value: int64(1)}),
		record.New(record.Field{Name: "k", Value: decimal.RequireFromString("1.0")}),
		record.New(record.Field{Name: "k", Value: nil}),
	}
	got := runGroup(t, map[string]any{"groupBy": []any{"k"}, "aggregates": []any{map[string]any{"name": "n", "function": "count"}}}, in...)
	if strings.Join(got, " ") != "{k: 1, n: 2} {k: null, n: 1}" {
		t.Errorf("keys: %v", got)
	}
}

func TestGroupByErrors(t *testing.T) {
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"aggregates": []any{map[string]any{"name": "x", "function": "median", "field": "a"}}}, `aggregates[0]: invalid function "median"`},
		{map[string]any{"aggregates": []any{map[string]any{"name": "x", "function": "sum"}}}, "aggregates[0]: sum needs a field"},
		{map[string]any{"groupBy": []any{"x"}, "aggregates": []any{map[string]any{"name": "x", "function": "count"}}}, `aggregates[0]: the field "x" would exist twice`},
	} {
		if _, err := groupByDefinition.New(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
	// A value that does not fit goes to the error link and counts in no group.
	tr := transformtest.New(t, groupByDefinition, map[string]any{"groupBy": []any{"country"},
		"aggregates": []any{map[string]any{"name": "total", "function": "sum", "field": "amount"}}})
	in := append(sales(), record.New(record.Field{Name: "country", Value: "IT"}, record.Field{Name: "amount", Value: "viel"}))
	out, failed, _, err := transformtest.RunWithErrorLink(context.Background(), tr, in...)
	if err != nil || len(out) != 3 || len(failed) != 1 {
		t.Fatalf("out %d, failed %d, %v", len(out), len(failed), err)
	}
	if msg, _ := failed[0].Get("_error"); msg != `record 6, field "amount": sum needs numbers, not string values` {
		t.Errorf("_error %v", msg)
	}
}

func TestGroupBySchema(t *testing.T) {
	tr := transformtest.New(t, groupByDefinition, map[string]any{"groupBy": []any{"country"}, "aggregates": []any{
		map[string]any{"name": "n", "function": "count"},
		map[string]any{"name": "total", "function": "sum", "field": "qty"},
		map[string]any{"name": "avg", "function": "avg", "field": "qty"},
		map[string]any{"name": "list", "function": "concat", "field": "city"},
		map[string]any{"name": "bad", "function": "sum", "field": "city"},
	}})
	in := record.Schema{}.With("country", record.String).With("city", record.String).With("qty", record.Int)
	ic := transform.NewInitContext(in)
	s, _ := tr.(transform.Initializer).Init(ic)
	want := []record.FieldDef{{Name: "country", Type: record.String}, {Name: "n", Type: record.Int}, {Name: "total", Type: record.Int},
		{Name: "avg", Type: record.Decimal}, {Name: "list", Type: record.String}, {Name: "bad", Type: record.String}}
	if s.Open || len(s.Fields) != len(want) {
		t.Fatalf("schema %+v", s)
	}
	for i := range want {
		if s.Fields[i] != want[i] {
			t.Errorf("field %d: %+v, want %+v", i, s.Fields[i], want[i])
		}
	}
	if p := ic.Problems(); len(p) != 1 || p[0].Path != "aggregates[4].field" || p[0].Message != `sum needs numbers, but "city" is string` {
		t.Errorf("problems %+v", p)
	}
	// Nothing configured is an error found before the run.
	tr = transformtest.New(t, groupByDefinition, nil)
	ic = transform.NewInitContext(in)
	_, _ = tr.(transform.Initializer).Init(ic)
	if p := ic.Problems(); len(p) != 1 || p[0].Warning || !strings.Contains(p[0].Message, "list the group fields") {
		t.Errorf("nothing: %+v", p)
	}
}
