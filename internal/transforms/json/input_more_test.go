package json

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

// utf16LE encodes text as UTF-16 little endian (characters of the Basic Multilingual
// Plane only), with a byte order mark if bom is set.
func utf16LE(s string, bom bool) string {
	var b []byte
	if bom {
		b = append(b, 0xFF, 0xFE)
	}
	for _, r := range s {
		b = append(b, byte(r), byte(r>>8))
	}
	return string(b)
}

// TestInputConfigErrors: mistakes in the configuration are reported before reading.
func TestInputConfigErrors(t *testing.T) {
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"path": "x.json", "format": "xml"}, `invalid format "xml" (expected auto, json or ndjson)`},
		{map[string]any{"path": "x.json", "fields": []any{map[string]any{"type": "int"}}}, `"fields[0].name"`},
		{map[string]any{"path": "x.json", "fields": []any{map[string]any{"name": "", "type": "int"}}}, "fields: every field needs a name"},
		{map[string]any{"path": "x.json", "fields": []any{map[string]any{"name": "a"}, map[string]any{"name": "a", "from": "b"}}}, `fields: the field "a" is listed twice`},
		{map[string]any{"path": "x.json", "fields": []any{map[string]any{"name": "a", "type": "money"}}}, `field "a": `},
		{map[string]any{"path": "x.json", "fields": []any{map[string]any{"name": "a", "type": "date", "format": "dd.QQ"}}}, `field "a": `},
		{map[string]any{"path": "x.json", "unknown": 1}, "unknown"},
	} {
		if _, err := newInput(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
}

// TestInputInit: with fields the schema is closed, with their types (unknown without a
// type); without fields it is open.
func TestInputInit(t *testing.T) {
	tr := transformtest.New(t, inputDefinition, map[string]any{"path": "x.json", "fields": []any{
		map[string]any{"name": "id", "type": "int"},
		map[string]any{"name": "city", "from": "address.city"},
		map[string]any{"name": "day", "type": "date", "format": "dd.MM.yyyy"},
	}})
	s, err := tr.(transform.Initializer).Init(transform.NewInitContext(record.Schema{}))
	if err != nil {
		t.Fatal(err)
	}
	want := record.Schema{}.With("id", record.Int).With("city", record.Unknown).With("day", record.DateType)
	if s.Open || fmt.Sprint(s) != fmt.Sprint(want) {
		t.Errorf("schema %v, want %v", s, want)
	}
	tr = transformtest.New(t, inputDefinition, map[string]any{"path": "x.json"})
	if s, err := tr.(transform.Initializer).Init(transform.NewInitContext(record.Schema{})); err != nil || !s.Open || len(s.Names()) != 0 {
		t.Errorf("without fields: %v %v", s, err)
	}
}

// TestInputFormats: the format follows the file name with auto and can be set; empty
// files and empty arrays give no records.
func TestInputFormats(t *testing.T) {
	for _, tt := range []struct {
		name, content string
		with          map[string]any
		want, log     string
	}{
		{"in.json", "{\"a\":1}\n{\"a\":2}\n", map[string]any{"format": "ndjson"}, "a=1(int64)\na=2(int64)\n", "read 2 records"},
		{"in.ndjson", "[{\"a\":1},\n{\"a\":2}]", map[string]any{"format": "json"}, "a=1(int64)\na=2(int64)\n", "read 2 records"},
		{"IN.JSONL", "{\"a\":1}\n", nil, "a=1(int64)\n", "read 1 record from"},
		{"in.json", "", nil, "", "read 0 records"},
		{"in.json", "  \r\n", nil, "", "read 0 records"},
		{"in.json", "[]", nil, "", "read 0 records"},
		{"in.ndjson", "\r\n\r\n", nil, "", "read 0 records"},
	} {
		with := map[string]any{"path": writeFile(t, tt.name, tt.content)}
		maps.Copy(with, tt.with)
		out, logs, err := transformtest.Run(context.Background(), transformtest.New(t, inputDefinition, with))
		if err != nil || show(out) != tt.want {
			t.Errorf("%s %q %v: got\n%s%v, want\n%s", tt.name, tt.content, tt.with, show(out), err, tt.want)
		}
		if len(logs) != 1 || !strings.HasPrefix(logs[0].Msg, tt.log) {
			t.Errorf("%s %q: logs %v, want %q", tt.name, tt.content, logs, tt.log)
		}
	}
}

// TestInputNested: values inside lists and objects are converted like those at the top:
// whole numbers int, others exact decimals; numbers too large for int stay exact; a list
// as an entry of the array becomes the field "value".
func TestInputNested(t *testing.T) {
	p := writeFile(t, "in.json", `[{"a": {"n": 1, "d": 2.5, "l": [3, 0.1, null, "x", true]}}, [1, {"x": 2.5}], 12345678901234567890]`)
	out, err := read(t, map[string]any{"path": p})
	if err != nil || len(out) != 3 {
		t.Fatalf("%v %v", out, err)
	}
	a, _ := out[0].Get("a")
	m, ok := a.(map[string]any)
	if !ok {
		t.Fatalf("a: %T", a)
	}
	l, _ := m["l"].([]any)
	got := fmt.Sprintf("%T %T %T %T %T %v %T", m["n"], m["d"], l[0], l[1], l[2], l[3:], m["l"])
	if want := "int64 decimal.Decimal int64 decimal.Decimal <nil> [x true] []interface {}"; got != want {
		t.Errorf("nested types %s, want %s", got, want)
	}
	v, _ := out[1].Get("value")
	list, _ := v.([]any)
	if len(list) != 2 || fmt.Sprintf("%T", list[0]) != "int64" {
		t.Errorf("list entry: %#v", v)
	} else if x, _ := list[1].(map[string]any); fmt.Sprintf("%T %v", x["x"], x["x"]) != "decimal.Decimal 2.5" {
		t.Errorf("object in list: %#v", list[1])
	}
	if got := show(out[2:]); got != "value=12345678901234567890(decimal.Decimal)\n" {
		t.Errorf("large number: %s", got)
	}
}

// TestInputRecordsPath: records: leads to an array, an object or a single value; the
// rest of the document after it is not read; paths through other values are errors.
func TestInputRecordsPath(t *testing.T) {
	for _, tt := range []struct {
		content, records, want, err string
	}{
		{`{"data": [{"a": 1}, {"a": 2}], "after": broken`, "data", "a=1(int64)\na=2(int64)\n", ""},
		{`{"data": {"a": 1}}`, "data", "a=1(int64)\n", ""},
		{`{"skip": {"items": [9]}, "data": {"items": [7]}}`, "data.items", "value=7(int64)\n", ""},
		{`{"data": 5}`, "data.items", "", `records: "items" is not inside an object`},
		{`[{"data": []}]`, "data", "", `records: "data" is not inside an object`},
		{`{"skip": [1,}, "data": []}`, "data", "", "invalid character '}' looking for beginning of value"},
		{`{"data": [{"a": 1}`, "data", "", "the file ends in the middle of a value"},
		{`{"other": 1}`, "data", "", `records: the document has no key "data" on the path "data"`},
	} {
		out, err := read(t, map[string]any{"path": writeFile(t, "in.json", tt.content), "records": tt.records})
		if tt.err == "" {
			if err != nil || show(out) != tt.want {
				t.Errorf("%s / %s: got\n%s%v, want\n%s", tt.content, tt.records, show(out), err, tt.want)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tt.err) {
			t.Errorf("%s / %s: %v, want %q", tt.content, tt.records, err, tt.err)
		}
	}
}

// TestInputSyntaxErrors: broken JSON stops the pipeline with line and column, also in
// files with CRLF, a byte order mark or in UTF-16; records read before are passed on.
func TestInputSyntaxErrors(t *testing.T) {
	for _, tt := range []struct {
		name, content string
		n             int
		want          string
	}{
		{"crlf", "[\r\n{\"a\": 1},\r\n{\"a\" 2}\r\n]", 1, "in.json:3:6: expected colon after object key"},
		{"bom", "\xEF\xBB\xBF[\r\n{\"a\": 1,}\r\n]", 0, "in.json:2:9: invalid character '}'"},
		{"utf-16", utf16LE("[{\"ä\": 1},\r\n{\"a\" 2}]", true), 1, "in.json:2:6: expected colon after object key"},
		{"trailing comma", `[{"a": 1}, ]`, 1, "in.json:1:12: invalid character ']'"},
		{"missing comma", `[{"a": 1} {"a": 2}]`, 1, "in.json:1:11: invalid character '{' after array element"},
		{"open object", `{"a": 1`, 0, "the file ends in the middle of a value"},
		{"not utf-8", "[\"M\xFCller\"]", 0, "in.json: the file is not valid UTF-8; if it comes from a Windows program, set encoding: windows-1252"},
		{"number out of range", "[1e1000000000]", 0, "1e1000000000 is not a number"},
	} {
		out, err := read(t, map[string]any{"path": writeFile(t, "in.json", tt.content)})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v, want %q", tt.name, err, tt.want)
		}
		if len(out) != tt.n {
			t.Errorf("%s: %d records before the error, want %d", tt.name, len(out), tt.n)
		}
	}
}

// TestInputEncodings: Windows-1252 and UTF-16 without byte order mark when set; an
// unknown encoding is an error.
func TestInputEncodings(t *testing.T) {
	for _, tt := range []struct {
		name, content, encoding, want string
	}{
		{"windows-1252", "[{\"name\": \"M\xFCller \x80\"}]", "windows-1252", "name=Müller €(string)\n"},
		{"utf-16 without bom", utf16LE(`[{"name": "Jürgen"}]`, false), "utf-16", "name=Jürgen(string)\n"},
		{"utf-8 with bom", "\xEF\xBB\xBF[{\"name\": \"Jürgen\"}]", "utf-8", "name=Jürgen(string)\n"},
		{"ndjson windows-1252", "{\"name\": \"M\xFCller\"}\r\n", "windows-1252", "name=Müller(string)\n"},
	} {
		name := "in.json"
		if strings.HasPrefix(tt.name, "ndjson") {
			name = "in.ndjson"
		}
		out, err := read(t, map[string]any{"path": writeFile(t, name, tt.content), "encoding": tt.encoding})
		if err != nil || show(out) != tt.want {
			t.Errorf("%s: got\n%s%v, want\n%s", tt.name, show(out), err, tt.want)
		}
	}
	_, err := read(t, map[string]any{"path": writeFile(t, "in.json", "[1]"), "encoding": "latin9"})
	if err == nil || !strings.Contains(err.Error(), `invalid encoding "latin9"`) {
		t.Errorf("unknown encoding: %v", err)
	}
	_, err = read(t, map[string]any{"path": writeFile(t, "in.ndjson", "{\"a\": \"\xFC\"}\n")})
	if err == nil || !strings.Contains(err.Error(), "in.ndjson: the file is not valid UTF-8") {
		t.Errorf("ndjson not utf-8: %v", err)
	}
}

// TestInputNDJSONWithoutErrorLink: without an error link, a broken line stops the
// pipeline with its line; two values in a line are an error of that line.
func TestInputNDJSONWithoutErrorLink(t *testing.T) {
	p := writeFile(t, "in.ndjson", "{\"a\": 1}\n{\"a\": 2} {\"a\": 3}\n{\"a\": 4}\n")
	out, err := read(t, map[string]any{"path": p})
	if err == nil || !strings.Contains(err.Error(), "in.ndjson:2: more than one value in the line") {
		t.Errorf("err %v", err)
	}
	if show(out) != "a=1(int64)\n" {
		t.Errorf("records before the error: %s", show(out))
	}
	p = writeFile(t, "in.ndjson", "{\"id\": \"x\"}\n")
	_, err = read(t, map[string]any{"path": p, "fields": []any{map[string]any{"name": "id", "type": "int"}}})
	if err == nil || !strings.Contains(err.Error(), `in.ndjson:1: record 1, field "id"`) {
		t.Errorf("value that does not fit: %v", err)
	}
}

// TestInputFieldError: in json a value that does not fit its type names the record
// without a line.
func TestInputFieldError(t *testing.T) {
	p := writeFile(t, "in.json", `[{"id": "1"}, {"id": "x"}]`)
	tr := transformtest.New(t, inputDefinition, map[string]any{"path": p, "fields": []any{map[string]any{"name": "id", "type": "int"}}})
	out, failed, _, err := transformtest.RunWithErrorLink(context.Background(), tr)
	if err != nil || show(out) != "id=1(int64)\n" || len(failed) != 1 {
		t.Fatalf("out %s failed %v err %v", show(out), failed, err)
	}
	if v, _ := failed[0].Get("_error"); !strings.HasSuffix(strings.SplitN(fmt.Sprint(v), ": ", 2)[0], "in.json") || !strings.Contains(fmt.Sprint(v), `record 2, field "id"`) {
		t.Errorf("_error %v", v)
	}
	if v, _ := failed[0].Get("_errorField"); v != "id" {
		t.Errorf("_errorField %v", v)
	}
}

// TestInputMissingFile: the message names the file once and the reason without the
// operation of Go; a canceled run reads nothing.
func TestInputMissingFile(t *testing.T) {
	_, err := read(t, map[string]any{"path": filepath.Join(t.TempDir(), "none.json")})
	if err == nil || strings.Count(err.Error(), "none.json") != 1 || strings.Contains(err.Error(), "open ") {
		t.Errorf("missing file: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tr := transformtest.New(t, inputDefinition, map[string]any{"path": writeFile(t, "in.json", `[{"a": 1}]`)})
	if out, _, err := transformtest.Run(ctx, tr); !errors.Is(err, context.Canceled) || len(out) != 0 {
		t.Errorf("canceled: %v %v", out, err)
	}
}

// TestRegister: both transforms of the namespace are registered under their names.
func TestRegister(t *testing.T) {
	r := transform.NewRegistry()
	Register(r)
	for _, typ := range []string{"json.input", "json.output"} {
		if _, ok := r.Lookup(typ); !ok {
			t.Errorf("%s not registered", typ)
		}
	}
}
