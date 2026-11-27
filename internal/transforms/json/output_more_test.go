package json

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

// TestOutputConfigErrors: mistakes in the configuration are reported before writing.
func TestOutputConfigErrors(t *testing.T) {
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"path": "x.json", "format": "csv"}, `invalid format "csv" (expected auto, json or ndjson)`},
		{map[string]any{"path": "x.json", "ifExists": "append"}, `invalid ifExists "append" (expected replace or fail)`},
		{map[string]any{"path": "x.json", "fields": []any{map[string]any{"name": ""}}}, "fields: every field needs a name"},
		{map[string]any{"path": "x.json", "fields": []any{map[string]any{"name": "a"}, map[string]any{"name": "a", "as": "b"}}}, `fields: the field "a" is listed twice`},
	} {
		if _, err := newOutput(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
}

// TestOutputInit: the schema is passed on; a field that is not in the incoming records
// is reported at its place in the configuration.
func TestOutputInit(t *testing.T) {
	tr := transformtest.New(t, outputDefinition, map[string]any{"path": "x.json", "fields": []any{
		map[string]any{"name": "id"}, map[string]any{"name": "cty"},
	}})
	in := record.Schema{}.With("id", record.Int).With("city", record.String)
	ic := transform.NewInitContext(in)
	s, err := tr.(transform.Initializer).Init(ic)
	if err != nil || len(s.Names()) != 2 {
		t.Fatalf("schema %v %v", s, err)
	}
	p := ic.Problems()
	if len(p) != 1 || p[0].Path != "fields[1].name" || p[0].Warning || !strings.Contains(p[0].Message, `"cty"`) {
		t.Errorf("problems %+v", p)
	}
}

// TestOutputFormat: format: overrides the file name; several records in NDJSON are
// separated by line breaks.
func TestOutputFormat(t *testing.T) {
	r1 := record.New(record.Field{Name: "a", Value: int64(1)})
	r2 := record.New(record.Field{Name: "a", Value: int64(2)})
	if got := write(t, "out.json", map[string]any{"format": "ndjson"}, r1, r2); got != "{\"a\":1}\n{\"a\":2}\n" {
		t.Errorf("ndjson: %q", got)
	}
	if got := write(t, "out.ndjson", map[string]any{"format": "json"}, r1, r2); got != "[\n{\"a\":1},\n{\"a\":2}\n]\n" {
		t.Errorf("json: %q", got)
	}
	if got := write(t, "OUT.NDJSON", map[string]any{"format": "auto"}, r1); got != "{\"a\":1}\n" {
		t.Errorf("auto, upper case: %q", got)
	}
}

// TestOutputValues: floats, times with zone and nanoseconds, maps (keys sorted) and
// lists, binary data (base64) and control characters.
func TestOutputValues(t *testing.T) {
	r := record.New(
		record.Field{Name: "f", Value: 1.5},
		record.Field{Name: "t", Value: time.Date(2026, 11, 19, 8, 30, 0, 5, time.FixedZone("", 3600))},
		record.Field{Name: "m", Value: map[string]any{"b": int64(1), "a": []any{"x", nil}}},
		record.Field{Name: "bin", Value: []byte("hi")},
		record.Field{Name: "s", Value: "\x01\x1f\r\t\\\"é"},
	)
	got := write(t, "out.ndjson", map[string]any{}, r)
	want := `{"f":1.5,"t":"2026-11-19T08:30:00.000000005+01:00","m":{"a":["x",null],"b":1},"bin":"aGk=","s":"\u0001\u001f\r\t\\\"é"}` + "\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestOutputRecordErrors: values that JSON cannot hold go to the error link with the
// record and the field and are not written; the other records are.
func TestOutputRecordErrors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "out.ndjson")
	tr := transformtest.New(t, outputDefinition, map[string]any{"path": p})
	in := []record.Record{
		record.New(record.Field{Name: "f", Value: math.NaN()}),
		record.New(record.Field{Name: "f", Value: 2.0}),
		record.New(record.Field{Name: "id", Value: int64(3)}, record.Field{Name: "f", Value: math.Inf(1)}),
		record.New(record.Field{Name: "c", Value: make(chan int)}),
	}
	out, failed, logs, err := transformtest.RunWithErrorLink(context.Background(), tr, in...)
	if err != nil || len(out) != 1 || len(failed) != 3 {
		t.Fatalf("out %v failed %v err %v", out, failed, err)
	}
	for i, want := range []string{`out.ndjson: record 1, field "f": NaN cannot be written in JSON`, `record 3, field "f": +Inf cannot be written in JSON`, `record 4, field "c": the value cannot be written in JSON`} {
		if v, _ := failed[i].Get(transform.ErrorField); !strings.Contains(v.(string), want) {
			t.Errorf("failed[%d]: %v, want %q", i, v, want)
		}
	}
	if b, _ := os.ReadFile(p); string(b) != "{\"f\":2}\n" {
		t.Errorf("file %q", b)
	}
	if len(logs) != 1 || logs[0].Msg != "wrote 1 record to "+p {
		t.Errorf("logs %v", logs)
	}
}

// TestOutputFieldFormatError: a value that cannot be written with the format of its field
// (binary data as text) goes to the error link; an empty value stays null.
func TestOutputFieldFormatError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "out.ndjson")
	tr := transformtest.New(t, outputDefinition, map[string]any{"path": p, "fields": []any{
		map[string]any{"name": "id"}, map[string]any{"name": "day", "as": "due.day", "format": "dd.MM.yyyy"},
	}})
	in := []record.Record{
		record.New(record.Field{Name: "id", Value: int64(1)}, record.Field{Name: "day", Value: []byte("x")}),
		record.New(record.Field{Name: "id", Value: int64(2)}, record.Field{Name: "day", Value: nil}),
	}
	out, failed, _, err := transformtest.RunWithErrorLink(context.Background(), tr, in...)
	if err != nil || len(out) != 1 || len(failed) != 1 {
		t.Fatalf("out %v failed %v err %v", out, failed, err)
	}
	if v, _ := failed[0].Get(transform.ErrorFieldField); v != "day" {
		t.Errorf("_errorField %v (%v)", v, failed[0])
	}
	if b, _ := os.ReadFile(p); string(b) != "{\"id\":2,\"due\":{\"day\":null}}\n" {
		t.Errorf("file %q", b)
	}
}

// TestOutputKeepsOldFile: without an error link a bad record stops the pipeline; the
// existing file stays as it was and no temporary file is left.
func TestOutputKeepsOldFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.json")
	if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	tr := transformtest.New(t, outputDefinition, map[string]any{"path": p})
	_, _, err := transformtest.Run(context.Background(), tr, order(1, "1", day, nil), record.New(record.Field{Name: "f", Value: math.NaN()}))
	if err == nil || !strings.Contains(err.Error(), `record 2, field "f"`) {
		t.Errorf("err %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "old" {
		t.Errorf("file changed: %q", b)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("temporary files left: %v", entries)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := transformtest.Run(ctx, tr, order(1, "1", day, nil)); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "old" {
		t.Errorf("file changed after cancel: %q", b)
	}
}

// TestOutputReplace: ifExists: replace (the default) replaces an existing file; a
// folder or a missing folder is an error.
func TestOutputReplace(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.json")
	if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	tr := transformtest.New(t, outputDefinition, map[string]any{"path": p, "ifExists": "replace"})
	if _, _, err := transformtest.Run(context.Background(), tr, record.New(record.Field{Name: "a", Value: int64(1)})); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "[\n{\"a\":1}\n]\n" {
		t.Errorf("file %q", b)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("temporary files left: %v", entries)
	}
	for _, tt := range []struct{ path, want string }{
		{dir, "it is a folder"},
		{filepath.Join(dir, "missing", "out.json"), "cannot write"},
	} {
		tr := transformtest.New(t, outputDefinition, map[string]any{"path": tt.path})
		if _, _, err := transformtest.Run(context.Background(), tr); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v, want %q", tt.path, err, tt.want)
		}
	}
}
