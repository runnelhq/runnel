package core

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

func TestGenerateRows(t *testing.T) {
	tr := transformtest.New(t, generateRowsDefinition, map[string]any{
		"rows": "3", "rowNumberField": "n",
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
		{"rows": "1", "fields": []any{map[string]any{"value": 1}}}, {}, {"rows": 5},
	} {
		if _, err := generateRowsDefinition.New(with); err == nil {
			t.Errorf("%v accepted", with)
		}
	}
	// The number is checked when the pipeline runs: it may be a variable before.
	for _, rows := range []string{"-1", "many", "${ROWS}"} {
		tr := transformtest.New(t, generateRowsDefinition, map[string]any{"rows": rows})
		if _, _, err := transformtest.Run(context.Background(), tr); err == nil || !strings.Contains(err.Error(), "is not a whole number") {
			t.Errorf("rows %q: %v", rows, err)
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

func TestGenerateRowsSchema(t *testing.T) {
	tr := transformtest.New(t, generateRowsDefinition, map[string]any{
		"rows": "1", "rowNumberField": "n",
		"fields": []any{
			map[string]any{"name": "status", "value": "new"},
			map[string]any{"name": "count", "value": uint64(3)},
			map[string]any{"name": "rate", "value": 1.5},
			map[string]any{"name": "ok", "value": true},
			map[string]any{"name": "nothing", "value": nil},
		},
	})
	s, err := tr.(transform.Initializer).Init(transform.NewInitContext(record.Schema{}))
	if err != nil {
		t.Fatal(err)
	}
	want := "n:int status:string count:int rate:float ok:bool nothing:"
	var got []string
	for _, f := range s.Fields {
		got = append(got, f.Name+":"+string(f.Type))
	}
	if strings.Join(got, " ") != want || s.Open {
		t.Errorf("schema %v (open %v), want %s", got, s.Open, want)
	}
	// Values are normalized: the YAML number becomes an int64.
	out, _, _ := transformtest.Run(context.Background(), tr)
	if v, _ := out[0].Get("count"); v != int64(3) {
		t.Errorf("count = %#v", v)
	}
}

func TestLogSelectedFieldsAndValueTypes(t *testing.T) {
	tr := transformtest.New(t, logDefinition, map[string]any{"fields": []any{"amount", "day", "missing", "raw"}})
	in := record.New(
		record.Field{Name: "id", Value: int64(1)},
		record.Field{Name: "amount", Value: decimal.RequireFromString("19.90")},
		record.Field{Name: "day", Value: record.Date{Year: 2026, Month: time.November, Day: 20}},
		record.Field{Name: "raw", Value: []byte("abc")},
	)
	out, logs, err := transformtest.Run(context.Background(), tr, in)
	if err != nil || len(out) != 1 || out[0].Len() != 4 {
		t.Fatalf("%v %v", out, err)
	}
	if len(logs) != 1 || logs[0].Msg != "#1 {amount: 19.9, day: 2026-11-20, raw: <3 bytes>}" {
		t.Errorf("logs %v", logs)
	}
	// Init checks the fields against the incoming schema and passes the schema on.
	ic := transform.NewInitContext(record.Schema{Fields: []record.FieldDef{{Name: "amount"}, {Name: "day"}, {Name: "raw"}}})
	s, _ := tr.(transform.Initializer).Init(ic)
	if p := ic.Problems(); len(p) != 1 || p[0].Path != "fields[2]" || len(s.Fields) != 3 {
		t.Errorf("problems %+v, schema %+v", p, s)
	}
}
