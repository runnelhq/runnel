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

// show writes records as one line each, with the Go type of every value.
func show(rs []record.Record) string {
	var b strings.Builder
	for _, r := range rs {
		for i, f := range r.Fields() {
			if i > 0 {
				b.WriteString(" ")
			}
			fmt.Fprintf(&b, "%s=%v(%T)", f.Name, f.Value, f.Value)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, with map[string]any) ([]record.Record, error) {
	t.Helper()
	out, _, err := transformtest.Run(context.Background(), transformtest.New(t, inputDefinition, with))
	return out, err
}

// TestInputArray: the keys keep their order, whole numbers are int, others exact
// decimals; null, lists and objects stay.
func TestInputArray(t *testing.T) {
	p := writeFile(t, "in.json", `[
  {"id": 1, "name": "Ada", "amount": 19.99, "paid": true, "note": null, "tags": ["a","b"], "address": {"city": "Köln"}},
  {"name": "Grace", "id": 2, "amount": 1e3}
]`)
	out, err := read(t, map[string]any{"path": p})
	if err != nil {
		t.Fatal(err)
	}
	want := "id=1(int64) name=Ada(string) amount=19.99(decimal.Decimal) paid=true(bool) note=<nil>(<nil>) tags=[a b]([]interface {}) address=map[city:Köln](map[string]interface {})\n" +
		"name=Grace(string) id=2(int64) amount=1000(decimal.Decimal)\n"
	if got := show(out); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestInputValues: several documents one after another (objects and arrays), and values
// that are no objects.
func TestInputValues(t *testing.T) {
	p := writeFile(t, "in.json", "{\"a\": 1}\n[{\"a\": 2}, 3]\n\"x\"")
	out, err := read(t, map[string]any{"path": p})
	if err != nil {
		t.Fatal(err)
	}
	if got := show(out); got != "a=1(int64)\na=2(int64)\nvalue=3(int64)\nvalue=x(string)\n" {
		t.Errorf("got\n%s", got)
	}
}

// TestInputRecordsAndFields: an array inside the document (an API answer), fields with
// a path into nested objects and a type.
func TestInputRecordsAndFields(t *testing.T) {
	p := writeFile(t, "in.json", `{"meta": {"page": 1, "skip": [1,2]}, "data": {"items": [
  {"id": "7", "customer": {"name": "Ada", "address": {"city": "Köln"}}, "day": "19.11.2026"},
  {"id": "8", "customer": {"name": "Grace"}, "day": "20.11.2026"}
]}, "after": {}}`)
	out, err := read(t, map[string]any{"path": p, "records": "data.items", "fields": []any{
		map[string]any{"name": "id", "type": "int"},
		map[string]any{"name": "city", "from": "customer.address.city"},
		map[string]any{"name": "day", "type": "date", "format": "dd.MM.yyyy"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := "id=7(int64) city=Köln(string) day=2026-11-19(record.Date)\nid=8(int64) city=<nil>(<nil>) day=2026-11-20(record.Date)\n"
	if got := show(out); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if _, err := read(t, map[string]any{"path": p, "records": "data.rows"}); err == nil || !strings.Contains(err.Error(), `no key "rows" on the path "data.rows"`) {
		t.Errorf("missing key: %v", err)
	}
}

// TestInputNDJSON: one object per line, empty lines skipped; a broken line and a value
// that does not fit go to the error link, with the line.
func TestInputNDJSON(t *testing.T) {
	p := writeFile(t, "in.ndjson", "{\"id\": 1}\r\n\r\n{\"id\": \n{\"id\": \"x\"}\n{\"id\": 4}\n")
	tr := transformtest.New(t, inputDefinition, map[string]any{"path": p, "fields": []any{map[string]any{"name": "id", "type": "int"}}})
	out, failed, logs, err := transformtest.RunWithErrorLink(context.Background(), tr)
	if err != nil {
		t.Fatal(err)
	}
	if got := show(out); got != "id=1(int64)\nid=4(int64)\n" {
		t.Errorf("out\n%s", got)
	}
	if len(failed) != 2 {
		t.Fatalf("failed %v", failed)
	}
	if v, _ := failed[0].Get("_error"); !strings.Contains(fmt.Sprint(v), "in.ndjson:3: ") {
		t.Errorf("_error of the broken line: %v", v)
	}
	if v, _ := failed[0].Get("line"); v != `{"id":` {
		t.Errorf("line: %v", v)
	}
	if v, _ := failed[1].Get("_error"); !strings.Contains(fmt.Sprint(v), `in.ndjson:4: record 3, field "id"`) {
		t.Errorf("_error of the value: %v", v)
	}
	if len(logs) != 1 || logs[0].Msg != "read 4 records from "+p+", 2 with errors" {
		t.Errorf("logs %v", logs)
	}
}

// TestInputErrors: syntax errors with line and column, duplicate keys, a missing file.
func TestInputErrors(t *testing.T) {
	for _, tt := range []struct{ content, want string }{
		{"[\n  {\"a\": 1},\n  {\"a\": 2,}\n]", "in.json:3:"},
		{"[{\"a\": 1}", "the file ends in the middle of a value"},
		{"{\"a\": 1, \"a\": 2}", `the key "a" appears twice`},
		{"\xFF\xFE", ""},
	} {
		p := writeFile(t, "in.json", tt.content)
		_, err := read(t, map[string]any{"path": p})
		if tt.want == "" {
			if err != nil {
				t.Errorf("%q: %v", tt.content, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%q: %v, want %q", tt.content, err, tt.want)
		}
	}
	if _, err := read(t, map[string]any{"path": filepath.Join(t.TempDir(), "none.json")}); err == nil || !strings.Contains(err.Error(), `cannot read`) {
		t.Errorf("missing file: %v", err)
	}
	if _, err := newInput(map[string]any{"path": "x.json", "format": "ndjson", "records": "a"}); err == nil {
		t.Error("records with ndjson: no error")
	}
}

// TestInputUTF16: JSON written by Windows PowerShell 5 (UTF-16 with byte order mark).
func TestInputUTF16(t *testing.T) {
	s := `[{"name":"Jürgen"}]`
	b := []byte{0xFF, 0xFE}
	for _, r := range s {
		b = append(b, byte(r), byte(r>>8))
	}
	p := writeFile(t, "in.json", string(b))
	out, err := read(t, map[string]any{"path": p})
	if err != nil || show(out) != "name=Jürgen(string)\n" {
		t.Errorf("%s %v", show(out), err)
	}
}

func BenchmarkInput(b *testing.B) {
	var sb strings.Builder
	sb.WriteString("[")
	for i := range 1000 {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"id": %d, "name": "customer %d", "amount": %d.50, "paid": true}`, i, i, i)
	}
	sb.WriteString("]")
	p := filepath.Join(b.TempDir(), "in.json")
	if err := os.WriteFile(p, []byte(sb.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	tr, err := newInput(map[string]any{"path": p})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := transformtest.Run(context.Background(), tr); err != nil {
			b.Fatal(err)
		}
	}
}
