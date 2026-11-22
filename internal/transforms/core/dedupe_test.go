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

func contact(id int64, email string, amount any) record.Record {
	return record.New(record.Field{Name: "id", Value: id}, record.Field{Name: "email", Value: email}, record.Field{Name: "amount", Value: amount})
}

func dedupeIDs(t *testing.T, with map[string]any, in ...record.Record) ([]int64, []transformtest.Logged) {
	t.Helper()
	out, logs, err := transformtest.Run(context.Background(), transformtest.New(t, dedupeDefinition, with), in...)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, r := range out {
		v, _ := r.Get("id")
		ids = append(ids, v.(int64))
	}
	return ids, logs
}

func TestDedupe(t *testing.T) {
	in := []record.Record{
		contact(1, "a@example.com", int64(10)),
		contact(2, "A@example.com", decimal.RequireFromString("10.0")),
		contact(3, "b@example.com", nil),
		contact(4, "a@example.com", int64(10)),
		contact(5, "b@example.com", nil),
	}
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"fields": []any{"email"}}, "[1 2 3]"},
		{map[string]any{"fields": []any{"email"}, "ignoreCase": true}, "[1 3]"},
		// 10 and 10.0 are the same number; empty values are equal to each other.
		{map[string]any{"fields": []any{"amount"}}, "[1 3]"},
		{map[string]any{"fields": []any{"email", "amount"}}, "[1 2 3]"},
		// Without fields: all fields (the id differs, so nothing is a duplicate).
		{map[string]any{}, "[1 2 3 4 5]"},
	} {
		if ids, _ := dedupeIDs(t, tt.with, in...); fmt.Sprint(ids) != tt.want {
			t.Errorf("%v: ids %v, want %s", tt.with, ids, tt.want)
		}
	}
	_, logs := dedupeIDs(t, map[string]any{"fields": []any{"email"}}, in...)
	if len(logs) != 1 || logs[0].Msg != "kept 3 of 5 records, removed 2 duplicates" {
		t.Errorf("log %v", logs)
	}
}

// TestDedupeAllFields: without fields the order of the fields does not matter, and
// values cannot run into each other ("a" + "bc" is not "ab" + "c").
func TestDedupeAllFields(t *testing.T) {
	r := func(id int64, a, b string) record.Record {
		return record.New(record.Field{Name: "a", Value: a}, record.Field{Name: "b", Value: b}, record.Field{Name: "n", Value: id})
	}
	swapped := record.New(record.Field{Name: "n", Value: int64(1)}, record.Field{Name: "b", Value: "x"}, record.Field{Name: "a", Value: "y"})
	out, _, err := transformtest.Run(context.Background(), transformtest.New(t, dedupeDefinition, map[string]any{}),
		r(1, "y", "x"), swapped, r(1, "yx", ""), r(1, "y\x00", "x"))
	if err != nil || len(out) != 3 {
		t.Errorf("%d records, want 3 (only the swapped one is a duplicate): %v", len(out), err)
	}
}

func TestDedupeSchema(t *testing.T) {
	tr := transformtest.New(t, dedupeDefinition, map[string]any{"fields": []any{"emial"}})
	ic := transform.NewInitContext(record.Schema{}.With("email", record.String))
	if _, err := tr.(transform.Initializer).Init(ic); err != nil {
		t.Fatal(err)
	}
	if p := ic.Problems(); len(p) != 1 || p[0].Path != "fields[0]" || !strings.Contains(p[0].Message, `did you mean "email"?`) {
		t.Errorf("problems %+v", p)
	}
}

func BenchmarkDedupe(b *testing.B) {
	tr, err := newDedupe(map[string]any{"fields": []any{"email"}})
	if err != nil {
		b.Fatal(err)
	}
	in := make([]record.Record, 1000)
	b.ReportAllocs()
	for b.Loop() {
		for i := range in {
			in[i] = contact(int64(i), fmt.Sprintf("%d@example.com", i%500), int64(i))
		}
		if _, _, err := transformtest.Run(context.Background(), tr, in...); err != nil {
			b.Fatal(err)
		}
	}
}
