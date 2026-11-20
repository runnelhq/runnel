package core

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

func TestGenerateRows(t *testing.T) {
	tr := transformtest.New(t, generateRowsDefinition, map[string]any{
		"rows": 3, "rowNumberField": "n",
		"fields": []any{map[string]any{"name": "status", "value": "new"}, map[string]any{"name": "amount", "value": 1.5}},
	})
	out, _, err := transformtest.Run(context.Background(), tr)
	if err != nil || len(out) != 3 {
		t.Fatalf("%d records, %v", len(out), err)
	}
	if got := Format(out[2]); got != `{n: 3, status: "new", amount: 1.5}` {
		t.Errorf("record %s", got)
	}
	for _, with := range []map[string]any{
		{"rows": -1}, {"rows": 1, "fields": []any{map[string]any{"value": 1}}}, {}, {"rows": "many"},
	} {
		if _, err := generateRowsDefinition.New(with); err == nil {
			t.Errorf("%v accepted", with)
		}
	}
}

func TestDummyPassesRecordsOn(t *testing.T) {
	tr := transformtest.New(t, dummyDefinition, nil)
	in := []record.Record{record.New(record.Field{Name: "a", Value: 1}), record.New(record.Field{Name: "a", Value: 2})}
	out, _, err := transformtest.Run(context.Background(), tr, in...)
	if err != nil || len(out) != 2 || Format(out[1]) != "{a: 2}" {
		t.Errorf("%v %v", out, err)
	}
	if _, err := dummyDefinition.New(map[string]any{"x": 1}); err == nil {
		t.Error("configuration accepted")
	}
}

func TestLogWritesEveryNthRecord(t *testing.T) {
	tr := transformtest.New(t, logDefinition, map[string]any{"every": 2, "level": "warn"})
	var in []record.Record
	for i := 1; i <= 5; i++ {
		in = append(in, record.New(record.Field{Name: "i", Value: i}, record.Field{Name: "at", Value: time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)}, record.Field{Name: "x", Value: nil}))
	}
	out, logs, err := transformtest.Run(context.Background(), tr, in...)
	if err != nil || len(out) != 5 {
		t.Fatalf("%d %v", len(out), err)
	}
	if len(logs) != 2 || logs[0].Level != slog.LevelWarn || logs[1].Msg != "#4 {i: 4, at: 2026-11-20T00:00:00Z, x: null}" {
		t.Errorf("logs %v", logs)
	}
	if _, err := logDefinition.New(map[string]any{"level": "loud"}); err == nil || !strings.Contains(err.Error(), `invalid level "loud"`) {
		t.Errorf("level: %v", err)
	}
	if _, err := logDefinition.New(map[string]any{"every": -1}); err == nil {
		t.Error("every -1 accepted")
	}
}
