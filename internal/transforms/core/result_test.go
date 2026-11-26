package core

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

func TestFromResult(t *testing.T) {
	rows := []record.Record{record.New(record.Field{Name: "path", Value: "a.csv"}), record.New(record.Field{Name: "path", Value: "b.csv"})}
	tr := transformtest.New(t, fromResultDefinition, nil)
	var out []record.Record
	o := transform.NewOutput("t", func(r record.Record) error { out = append(out, r); return nil }, nil).WithResults(rows, nil)
	if err := tr.Process(context.Background(), nil, o); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || Format(out[1]) != `{path: "b.csv"}` {
		t.Fatalf("out %v", out)
	}
	// The records given to the run are not changed by later transforms.
	out[0].Set("path", "x")
	if v, _ := rows[0].Get("path"); v != "a.csv" {
		t.Error("the given record was changed")
	}
	// Without records: none, with a message.
	_, logs, err := transformtest.Run(context.Background(), tr)
	if err != nil || len(logs) != 1 || logs[0].Level != slog.LevelInfo || !strings.Contains(logs[0].Msg, "no records were given") {
		t.Errorf("%v %v", logs, err)
	}
}

func TestToResult(t *testing.T) {
	var results []record.Record
	run := func(with map[string]any, in ...record.Record) ([]record.Record, error) {
		results = nil
		tr := transformtest.New(t, toResultDefinition, with)
		ch := make(chan transform.Batch, len(in))
		for _, r := range in {
			ch <- transform.Batch{r}
		}
		close(ch)
		var out []record.Record
		o := transform.NewOutput("t", func(r record.Record) error { out = append(out, r); return nil }, nil).
			WithResults(nil, func(r record.Record) error { results = append(results, r); return nil })
		return out, tr.Process(context.Background(), ch, o)
	}
	in := []record.Record{record.New(record.Field{Name: "n", Value: int64(1)}), record.New(record.Field{Name: "n", Value: int64(2)})}
	out, err := run(nil, in...)
	if err != nil || len(out) != 2 || len(results) != 2 {
		t.Fatalf("out %d, results %d, %v", len(out), len(results), err)
	}
	// The result holds copies: changes after the transform do not reach it.
	out[0].Set("n", int64(9))
	if v, _ := results[0].Get("n"); v != int64(1) {
		t.Error("result shares the record")
	}
	if _, err := run(map[string]any{"maxRecords": "1"}, in...); err == nil || !strings.Contains(err.Error(), "more than 1 records for the result (maxRecords)") {
		t.Errorf("limit: %v", err)
	}
	// An invalid limit is an error of the configuration (number fields, R5b2).
	for limit, want := range map[string]string{"0": `maxRecords: "0" is not a whole number of 1 or more`,
		"many": `field "maxRecords": expected a whole number, got "many"`} {
		if _, err := toResultDefinition.New(map[string]any{"maxRecords": limit}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("limit %q: %v", limit, err)
		}
	}
	if _, err := toResultDefinition.New(map[string]any{"maxRecords": "${N}"}); err != nil {
		t.Errorf("variable: %v", err)
	}
}
