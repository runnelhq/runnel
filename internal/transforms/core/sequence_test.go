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

func position(order int64, item string) record.Record {
	return record.New(record.Field{Name: "order", Value: order}, record.Field{Name: "item", Value: item})
}

func sequenceNumbers(t *testing.T, with map[string]any, field string, in ...record.Record) string {
	t.Helper()
	out, _, err := transformtest.Run(context.Background(), transformtest.New(t, sequenceDefinition, with), in...)
	if err != nil {
		t.Fatalf("%v: %v", with, err)
	}
	var n []any
	for _, r := range out {
		v, _ := r.Get(field)
		n = append(n, v)
	}
	return fmt.Sprint(n)
}

func TestSequence(t *testing.T) {
	in := []record.Record{position(7, "pen"), position(7, "ink"), position(8, "pad"), position(7, "cap")}
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"name": "n"}, "[1 2 3 4]"},
		{map[string]any{"name": "n", "start": "10", "step": "10"}, "[10 20 30 40]"},
		{map[string]any{"name": "n", "start": "0", "step": "-1"}, "[0 -1 -2 -3]"},
		// Per order; the groups need not be sorted.
		{map[string]any{"name": "n", "groupBy": []any{"order"}}, "[1 2 1 3]"},
		{map[string]any{"name": "n", "groupBy": []any{"order"}, "start": "10", "step": "10"}, "[10 20 10 30]"},
	} {
		if got := sequenceNumbers(t, tt.with, "n", in...); got != tt.want {
			t.Errorf("%v: %s, want %s", tt.with, got, tt.want)
		}
	}
	// An existing field gets the number.
	if got := sequenceNumbers(t, map[string]any{"name": "item"}, "item", in...); got != "[1 2 3 4]" {
		t.Errorf("existing field: %s", got)
	}
}

func TestSequenceErrors(t *testing.T) {
	if _, err := newSequence(map[string]any{"name": " "}); err == nil || !strings.Contains(err.Error(), "needs a name") {
		t.Errorf("empty name: %v", err)
	}
	// start and step are checked with the configuration; a variable once it is resolved
	// (number fields, R5b2).
	if _, err := newSequence(map[string]any{"name": "n", "step": "ten"}); err == nil || err.Error() != `field "step": expected a whole number, got "ten"` {
		t.Errorf("step: %v", err)
	}
	if _, err := newSequence(map[string]any{"name": "n", "start": "${START}", "step": "${STEP}"}); err != nil {
		t.Errorf("variables: %v", err)
	}
}

func TestSequenceSchema(t *testing.T) {
	tr := transformtest.New(t, sequenceDefinition, map[string]any{"name": "n", "groupBy": []any{"ordr"}})
	ic := transform.NewInitContext(record.Schema{}.With("order", record.Int))
	s, err := tr.(transform.Initializer).Init(ic)
	if f, ok := s.Field("n"); err != nil || !ok || f.Type != record.Int {
		t.Errorf("schema %v %v", s.Names(), err)
	}
	if p := ic.Problems(); len(p) != 1 || p[0].Path != "groupBy[0]" || !strings.Contains(p[0].Message, `did you mean "order"?`) {
		t.Errorf("problems %+v", p)
	}
}

func BenchmarkSequence(b *testing.B) {
	tr, err := newSequence(map[string]any{"name": "n", "groupBy": []any{"order"}})
	if err != nil {
		b.Fatal(err)
	}
	in := make([]record.Record, 1000)
	b.ReportAllocs()
	for b.Loop() {
		for i := range in {
			in[i] = position(int64(i%50), "pen")
		}
		if _, _, err := transformtest.Run(context.Background(), tr, in...); err != nil {
			b.Fatal(err)
		}
	}
}
