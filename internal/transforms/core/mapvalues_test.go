package core

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

func statusRecord(v any) record.Record {
	return record.New(record.Field{Name: "status", Value: v})
}

func mapped(t *testing.T, with map[string]any, field string, values ...any) string {
	t.Helper()
	var in []record.Record
	for _, v := range values {
		in = append(in, statusRecord(v))
	}
	out, _, err := transformtest.Run(context.Background(), transformtest.New(t, mapValuesDefinition, with), in...)
	if err != nil {
		t.Fatalf("%v: %v", with, err)
	}
	var got []string
	for _, r := range out {
		v, ok := r.Get(field)
		got = append(got, fmt.Sprintf("%v(%v)", v, ok))
	}
	return strings.Join(got, " ")
}

func TestMapValues(t *testing.T) {
	table := []any{map[string]any{"from": "A", "to": "Active"}, map[string]any{"from": "I", "to": "Inactive"}, map[string]any{"from": "1", "to": 10}}
	for _, tt := range []struct {
		with  map[string]any
		field string
		want  string
	}{
		// In place; a value that is not in the table stays; numbers compare as text.
		{map[string]any{"field": "status", "values": table}, "status", "Active(true) X(true) 10(true) <nil>(true)"},
		{map[string]any{"field": "status", "values": table, "ignoreCase": true}, "status", "Active(true) X(true) 10(true) <nil>(true)"},
		// Into another field, with a default also for the empty value.
		{map[string]any{"field": "status", "as": "text", "values": table, "default": "Unknown"}, "text", "Active(true) Unknown(true) 10(true) Unknown(true)"},
		// Without a default the target gets the value as it is.
		{map[string]any{"field": "status", "as": "text", "values": table}, "text", "Active(true) X(true) 10(true) <nil>(true)"},
	} {
		if got := mapped(t, tt.with, tt.field, "A", "X", int64(1), nil); got != tt.want {
			t.Errorf("%v: %s, want %s", tt.with, got, tt.want)
		}
	}
	if got := mapped(t, map[string]any{"field": "status", "values": table, "ignoreCase": true}, "status", "a", "i"); got != "Active(true) Inactive(true)" {
		t.Errorf("ignoreCase: %s", got)
	}
	if got := mapped(t, map[string]any{"field": "status", "values": []any{map[string]any{"from": "", "to": "-"}, map[string]any{"from": "X"}}}, "status", nil, "X"); got != "-(true) <nil>(true)" {
		t.Errorf("empty from / missing to: %s", got)
	}
}

func TestMapValuesErrors(t *testing.T) {
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"field": "s", "values": []any{}}, "list at least one value"},
		{map[string]any{"field": "s", "values": []any{map[string]any{"from": "A"}, map[string]any{"from": "a"}}, "ignoreCase": true}, `"a" is listed twice`},
		{map[string]any{"values": []any{map[string]any{"from": "A"}}}, "missing required field"},
	} {
		if _, err := newMapValues(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
}

func TestMapValuesSchema(t *testing.T) {
	for _, tt := range []struct {
		with map[string]any
		want record.Type
	}{
		{map[string]any{"field": "status", "as": "text", "values": []any{map[string]any{"from": "A", "to": "Active"}}, "default": "?"}, record.String},
		{map[string]any{"field": "status", "as": "text", "values": []any{map[string]any{"from": "A", "to": "Active"}}}, record.Unknown},
		{map[string]any{"field": "status", "as": "text", "values": []any{map[string]any{"from": "A", "to": 1}}, "default": "?"}, record.Unknown},
	} {
		tr := transformtest.New(t, mapValuesDefinition, tt.with)
		ic := transform.NewInitContext(record.Schema{}.With("status", record.String))
		s, err := tr.(transform.Initializer).Init(ic)
		if f, ok := s.Field("text"); err != nil || !ok || f.Type != tt.want {
			t.Errorf("%v: %+v %v, want %s", tt.with, f, err, tt.want)
		}
	}
	tr := transformtest.New(t, mapValuesDefinition, map[string]any{"field": "stauts", "values": []any{map[string]any{"from": "A"}}})
	ic := transform.NewInitContext(record.Schema{}.With("status", record.String))
	_, _ = tr.(transform.Initializer).Init(ic)
	if p := ic.Problems(); len(p) != 1 || p[0].Path != "field" || !strings.Contains(p[0].Message, `did you mean "status"?`) {
		t.Errorf("problems %+v", p)
	}
}

func BenchmarkMapValues(b *testing.B) {
	tr, err := newMapValues(map[string]any{"field": "status", "values": []any{map[string]any{"from": "A", "to": "Active"}, map[string]any{"from": "I", "to": "Inactive"}}, "default": "Unknown"})
	if err != nil {
		b.Fatal(err)
	}
	in := make([]record.Record, 1000)
	codes := []string{"A", "I", "X"}
	b.ReportAllocs()
	for b.Loop() {
		for i := range in {
			in[i] = statusRecord(codes[i%3])
		}
		if _, _, err := transformtest.Run(context.Background(), tr, in...); err != nil {
			b.Fatal(err)
		}
	}
}
