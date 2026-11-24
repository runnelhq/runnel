package json

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

// fields[].as names the key, with dots for nested objects that fields share; json.input
// reads them back with fields[].from (ADR 0025).
func TestOutputAs(t *testing.T) {
	got := write(t, "out.jsonl", map[string]any{"fields": []any{
		map[string]any{"name": "id", "as": "orderId"},
		map[string]any{"name": "amount", "as": "total.amount"},
		map[string]any{"name": "note"},
		map[string]any{"name": "day", "as": "total.due.day", "format": "dd.MM.yyyy"},
	}}, order(1, "19.90", day, nil))
	want := `{"orderId":1,"total":{"amount":19.9,"due":{"day":"19.11.2026"}},"note":null}` + "\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}

	p := filepath.Join(t.TempDir(), "in.jsonl")
	if err := os.WriteFile(p, []byte(got), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, err := transformtest.Run(context.Background(), transformtest.New(t, inputDefinition, map[string]any{"path": p, "fields": []any{
		map[string]any{"name": "id", "from": "orderId", "type": "int"},
		map[string]any{"name": "amount", "from": "total.amount", "type": "decimal"},
	}}))
	if err != nil || len(out) != 1 || fmt.Sprint(out[0].Fields()) != "[{id 1} {amount 19.9}]" {
		t.Errorf("read back: %v %v", out, err)
	}

	for _, tt := range []struct {
		fields []any
		want   string
	}{
		{[]any{map[string]any{"name": "a", "as": "x"}, map[string]any{"name": "x"}}, `field "x": the key "x" is written twice`},
		{[]any{map[string]any{"name": "a", "as": "x"}, map[string]any{"name": "b", "as": "x.y"}}, `field "b": the key "x.y" is written twice`},
		{[]any{map[string]any{"name": "a", "as": "x.y"}, map[string]any{"name": "b", "as": "x"}}, `field "b": the key "x" is written twice`},
		{[]any{map[string]any{"name": "a", "as": "x..y"}}, `field "a": as: "x..y" has an empty part`},
	} {
		if _, err := newOutput(map[string]any{"path": "x.json", "fields": tt.fields}); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.fields, err, tt.want)
		}
	}
}

// BenchmarkOutputFields writes 1,000 records with fields, flat and nested by as.
func BenchmarkOutputFields(b *testing.B) {
	for _, bm := range []struct {
		name   string
		fields []any
	}{
		{"flat", []any{map[string]any{"name": "id"}, map[string]any{"name": "amount"}, map[string]any{"name": "day"}, map[string]any{"name": "note"}}},
		{"nested", []any{map[string]any{"name": "id"}, map[string]any{"name": "amount", "as": "total.amount"}, map[string]any{"name": "day", "as": "total.day"}, map[string]any{"name": "note"}}},
	} {
		b.Run(bm.name, func(b *testing.B) {
			in := make([]record.Record, 1000)
			tr, err := newOutput(map[string]any{"path": filepath.Join(b.TempDir(), "out.json"), "fields": bm.fields})
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
		})
	}
}
