package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

type logged struct {
	level slog.Level
	msg   string
}

// run processes records with a waiting caller that accepts the first answer.
func run(t *testing.T, with map[string]any, input ...record.Record) ([]action.Response, []record.Record, []logged, error) {
	t.Helper()
	tr := transformtest.New(t, responseDefinition, with)
	var sent []action.Response
	var out []record.Record
	var logs []logged
	o := transform.NewOutput("answer", func(r record.Record) error { out = append(out, r); return nil },
		func(l slog.Level, m string) { logs = append(logs, logged{l, m}) }).
		WithRespond(func(r action.Response) bool { sent = append(sent, r); return len(sent) == 1 })
	in := make(chan transform.Batch, 1)
	in <- transform.Batch(input)
	close(in)
	err := tr.Process(context.Background(), in, o)
	return sent, out, logs, err
}

func body(r action.Response) string { return string(r.Body.(json.RawMessage)) }

func TestResponse(t *testing.T) {
	day, _ := record.ParseDate("2026-11-21")
	rows := []record.Record{
		record.New(record.Field{Name: "id", Value: int64(7)}, record.Field{Name: "name", Value: "Anna"},
			record.Field{Name: "amount", Value: decimal.RequireFromString("12.50")}, record.Field{Name: "day", Value: day},
			record.Field{Name: "at", Value: time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)}, record.Field{Name: "note", Value: nil}),
		record.New(record.Field{Name: "id", Value: int64(8)}, record.Field{Name: "name", Value: "Ben"}),
	}
	sent, out, logs, err := run(t, map[string]any{"status": "201", "headers": map[string]any{"X-Count": "2"}}, rows...)
	if err != nil || len(sent) != 1 || len(out) != 2 {
		t.Fatalf("%v %v %v", sent, out, err)
	}
	want := `[{"id":7,"name":"Anna","amount":12.5,"day":"2026-11-21","at":"2026-11-21T08:00:00Z","note":null},{"id":8,"name":"Ben"}]`
	if r := sent[0]; r.Status != 201 || body(r) != want || r.Headers["X-Count"] != "2" || r.Headers["Content-Type"] != "application/json; charset=utf-8" || r.Fallback {
		t.Errorf("answer %d %s %v", r.Status, body(r), r.Headers)
	}
	if len(logs) != 1 || logs[0].msg != "answered the caller with status 201 and 2 records" {
		t.Errorf("logs %v", logs)
	}

	// single: the first record as an object.
	sent, _, logs, _ = run(t, map[string]any{"single": true}, rows...)
	if body(sent[0]) != `{"id":7,"name":"Anna","amount":12.5,"day":"2026-11-21","at":"2026-11-21T08:00:00Z","note":null}` ||
		!strings.Contains(logs[len(logs)-1].msg, "single: 2 records, only the first was sent") {
		t.Errorf("single %s %v", body(sent[0]), logs)
	}

	// Without records: no answer, or the fallback with whenEmpty; with whenEmpty the
	// answer is always the fallback, also with records.
	if sent, _, _, _ := run(t, nil); len(sent) != 0 {
		t.Errorf("empty: %v", sent)
	}
	sent, _, _, _ = run(t, map[string]any{"status": "404", "whenEmpty": true, "single": true})
	if len(sent) != 1 || !sent[0].Fallback || sent[0].Status != 404 || body(sent[0]) != "null" {
		t.Errorf("whenEmpty %+v", sent)
	}
	sent, _, _, _ = run(t, map[string]any{"whenEmpty": true})
	if body(sent[0]) != "[]" {
		t.Errorf("whenEmpty list %s", body(sent[0]))
	}
	sent, _, _, _ = run(t, map[string]any{"whenEmpty": true, "status": "404"}, rows[1])
	if !sent[0].Fallback || body(sent[0]) != `[{"id":8,"name":"Ben"}]` {
		t.Errorf("whenEmpty with records %+v", sent[0])
	}

	// maxRecords limits the answer; it and status are checked with the configuration,
	// a variable once it is resolved (number fields, R5b2).
	if _, _, _, err := run(t, map[string]any{"maxRecords": "1"}, rows...); err == nil || !strings.Contains(err.Error(), "more than 1 records for the answer") {
		t.Errorf("maxRecords: %v", err)
	}
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"status": "abc"}, `field "status": expected a whole number, got "abc"`},
		{map[string]any{"status": "99"}, `invalid status "99" (expected a number from 200 to 599)`},
		{map[string]any{"status": "{{ 404 }}"}, `field "status": expected a whole number, got "{{ 404 }}"`},
		{map[string]any{"maxRecords": "0"}, `maxRecords: "0" is not a whole number of 1 or more`},
		{map[string]any{"maxRecords": "x"}, `field "maxRecords": expected a whole number, got "x"`},
	} {
		if _, err := responseDefinition.New(tt.with); err == nil || err.Error() != tt.want {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
	if _, err := responseDefinition.New(map[string]any{"status": "${STATUS}", "maxRecords": "${N}"}); err != nil {
		t.Errorf("variables: %v", err)
	}

	// Without a caller the records are passed on, nothing is sent.
	tr := transformtest.New(t, responseDefinition, nil)
	out, tlogs, err := transformtest.Run(context.Background(), tr, rows...)
	if err != nil || len(out) != 2 || len(tlogs) != 1 || !strings.Contains(tlogs[0].Msg, "no HTTP caller waits") {
		t.Errorf("without caller: %v %v %v", out, tlogs, err)
	}
	if _, err := newResponse(map[string]any{"code": 1}); err == nil || !strings.Contains(err.Error(), `unknown field "code"`) {
		t.Errorf("config: %v", err)
	}
}
