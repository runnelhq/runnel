package core

import (
	"context"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

func people() []record.Record {
	p := func(id int64, name string, amount any) record.Record {
		return record.New(record.Field{Name: "id", Value: id}, record.Field{Name: "name", Value: name}, record.Field{Name: "amount", Value: amount})
	}
	return []record.Record{
		p(1, "weber", decimal.RequireFromString("10.5")),
		p(2, "Adam", int64(10)),
		p(3, "müller", nil),
		p(4, "adam", 2.5),
		p(5, "Weber", decimal.RequireFromString("10.50")),
	}
}

func sortIDs(t *testing.T, with map[string]any) string {
	t.Helper()
	tr := transformtest.New(t, sortDefinition, with)
	out, _, err := transformtest.Run(context.Background(), tr, people()...)
	if err != nil {
		t.Fatal(err)
	}
	return ids(out)
}

func TestSort(t *testing.T) {
	for _, tt := range []struct {
		fields []any
		want   string
	}{
		// Numbers across int, decimal and float; equal values keep their order; null last.
		{[]any{map[string]any{"name": "amount"}}, "4 2 1 5 3"},
		{[]any{map[string]any{"name": "amount", "descending": true}}, "1 5 2 4 3"},
		// Text by code points (upper case first), or regardless of case.
		{[]any{map[string]any{"name": "name"}}, "2 5 4 3 1"},
		{[]any{map[string]any{"name": "name", "ignoreCase": true}, map[string]any{"name": "id", "descending": true}}, "4 2 3 5 1"},
	} {
		if got := sortIDs(t, map[string]any{"fields": tt.fields}); got != tt.want {
			t.Errorf("%v: %s, want %s", tt.fields, got, tt.want)
		}
	}
}

func TestSortErrors(t *testing.T) {
	if _, err := sortDefinition.New(map[string]any{"fields": []any{}}); err == nil || !strings.Contains(err.Error(), "list at least one field") {
		t.Errorf("no fields: %v", err)
	}
	tr := transformtest.New(t, sortDefinition, map[string]any{"fields": []any{map[string]any{"name": "amount"}}})
	in := append(people(), record.New(record.Field{Name: "id", Value: int64(6)}, record.Field{Name: "amount", Value: "viel"}))
	out, failed, _, err := transformtest.RunWithErrorLink(context.Background(), tr, in...)
	if err != nil || ids(out) != "4 2 1 5 3" || len(failed) != 1 {
		t.Fatalf("out %s, failed %v, %v", ids(out), failed, err)
	}
	if msg, _ := failed[0].Get("_error"); msg != `record 6, field "amount": string cannot be sorted together with number values before` {
		t.Errorf("_error %v", msg)
	}
	// Without an error link, the pipeline stops.
	if _, _, err := transformtest.Run(context.Background(), tr, in...); err == nil {
		t.Error("no error")
	}
}
