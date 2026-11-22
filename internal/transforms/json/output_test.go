package json

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

func order(id int64, amount string, day record.Date, note any) record.Record {
	return record.New(record.Field{Name: "id", Value: id}, record.Field{Name: "amount", Value: decimal.RequireFromString(amount)},
		record.Field{Name: "day", Value: day}, record.Field{Name: "note", Value: note})
}

var day = record.DateOf(time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC))

func write(t *testing.T, name string, with map[string]any, in ...record.Record) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	with["path"] = p
	out, _, err := transformtest.Run(context.Background(), transformtest.New(t, outputDefinition, with), in...)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Errorf("%d records passed on, want %d", len(out), len(in))
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestOutputJSON: an array with one record per line; keys in the order of the fields,
// exact decimals, ISO dates, text escaped.
func TestOutputJSON(t *testing.T) {
	got := write(t, "out.json", map[string]any{}, order(1, "19.90", day, "a \"b\"\n<c>"), order(2, "1000", day, nil))
	want := "[\n{\"id\":1,\"amount\":19.9,\"day\":\"2026-11-19\",\"note\":\"a \\\"b\\\"\\n<c>\"},\n{\"id\":2,\"amount\":1000,\"day\":\"2026-11-19\",\"note\":null}\n]\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if got := write(t, "empty.json", map[string]any{}); got != "[]\n" {
		t.Errorf("empty: %q", got)
	}
}

// TestOutputNDJSONAndFields: one object per line (by the file name), selected fields
// with a format as text.
func TestOutputNDJSONAndFields(t *testing.T) {
	got := write(t, "out.jsonl", map[string]any{"fields": []any{
		map[string]any{"name": "day", "format": "dd.MM.yyyy"},
		map[string]any{"name": "amount", "format": "#.##0,00"},
		map[string]any{"name": "missing"},
	}}, order(1, "1234.5", day, nil), order(2, "3", day, nil))
	want := "{\"day\":\"19.11.2026\",\"amount\":\"1.234,50\",\"missing\":null}\n{\"day\":\"19.11.2026\",\"amount\":\"3,00\",\"missing\":null}\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if got := write(t, "empty.ndjson", map[string]any{}); got != "" {
		t.Errorf("empty: %q", got)
	}
}

// TestOutputRoundTrip: what json.output writes, json.input reads back the same.
func TestOutputRoundTrip(t *testing.T) {
	r := record.New(record.Field{Name: "n", Value: int64(-7)}, record.Field{Name: "d", Value: decimal.RequireFromString("0.1")},
		record.Field{Name: "s", Value: "tab\there"}, record.Field{Name: "b", Value: false},
		record.Field{Name: "list", Value: []any{int64(1), "x"}})
	p := filepath.Join(t.TempDir(), "rt.json")
	if _, _, err := transformtest.Run(context.Background(), transformtest.New(t, outputDefinition, map[string]any{"path": p}), r); err != nil {
		t.Fatal(err)
	}
	out, err := read(t, map[string]any{"path": p})
	if err != nil || show(out) != show([]record.Record{r}) {
		t.Errorf("read back\n%s\nwant\n%s%v", show(out), show([]record.Record{r}), err)
	}
}

func TestOutputErrors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "exists.json")
	if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	tr := transformtest.New(t, outputDefinition, map[string]any{"path": p, "ifExists": "fail"})
	if _, _, err := transformtest.Run(context.Background(), tr, order(1, "1", day, nil)); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("ifExists fail: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "old" {
		t.Errorf("file changed: %q", b)
	}
	if _, err := newOutput(map[string]any{"path": "x.json", "fields": []any{map[string]any{"name": "a", "format": "dd.QQ"}}}); err == nil {
		t.Error("invalid format: no error")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("temporary files left: %v", entries)
	}
}

func BenchmarkOutput(b *testing.B) {
	in := make([]record.Record, 1000)
	p := filepath.Join(b.TempDir(), "out.json")
	tr, err := newOutput(map[string]any{"path": p})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		for i := range in {
			in[i] = order(int64(i), "19.90", day, "customer")
		}
		if _, _, err := transformtest.Run(context.Background(), tr, in...); err != nil {
			b.Fatal(err)
		}
	}
}
