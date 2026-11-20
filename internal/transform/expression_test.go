package transform

import (
	"slices"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/record"
)

func output(values map[string]any) *Output {
	return NewOutput("t", func(record.Record) error { return nil }, nil).WithValues(values)
}

// An expression is evaluated for many records with one context: nothing of a record
// must remain for the next one.
func TestExpressionPerRecord(t *testing.T) {
	out := output(map[string]any{"params": map[string]any{"MIN": int64(2)}})
	e, err := CompileExpression("{{ record.n >= params.MIN && record.extra == nil }}")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(e.fields, []string{"n", "extra"}) {
		t.Errorf("fields %v", e.fields)
	}
	for _, c := range []struct {
		rec  record.Record
		want bool
	}{
		{record.New(record.Field{Name: "n", Value: int64(3)}, record.Field{Name: "extra", Value: "x"}), false},
		{record.New(record.Field{Name: "n", Value: int64(3)}), true}, // extra of the record before is gone
		{record.New(record.Field{Name: "n", Value: int64(1)}), false},
		{record.New(record.Field{Name: "other", Value: int64(9)}), false},
	} {
		if got, err := e.Bool(out, c.rec); err != nil || got != c.want {
			t.Errorf("%v: got %v, %v; want %v", c.rec.Names(), got, err, c.want)
		}
	}
	// Other values of the run (a new run) are taken over.
	if got, _ := e.Bool(output(map[string]any{"params": map[string]any{"MIN": int64(5)}}), record.New(record.Field{Name: "n", Value: int64(3)})); got {
		t.Error("the values of the first run were kept")
	}
}

// An expression that may read any field gets all of them; dates are times.
func TestExpressionAllFields(t *testing.T) {
	out := output(map[string]any{})
	e, err := CompileExpression(`{{ len(record) == 2 && record["d"] < now() }}`)
	if err != nil {
		t.Fatal(err)
	}
	if e.fields != nil {
		t.Errorf("fields %v, want all", e.fields)
	}
	d := record.DateOf(time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC))
	rec := record.New(record.Field{Name: "d", Value: d}, record.Field{Name: "x", Value: "y"})
	if got, err := e.Bool(out, rec); err != nil || !got {
		t.Errorf("got %v, %v", got, err)
	}
	if got, _ := e.Bool(out, record.New(record.Field{Name: "d", Value: d})); got {
		t.Error("a field of the record before remained")
	}
}
