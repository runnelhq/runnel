package csv

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

var update = flag.Bool("update", false, "update golden files")

func orders() []record.Record {
	return []record.Record{
		record.New(
			record.Field{Name: "id", Value: int64(7)},
			record.Field{Name: "customer", Value: "Müller, Jörg"},
			record.Field{Name: "amount", Value: decimal.RequireFromString("1234.5")},
			record.Field{Name: "day", Value: record.DateOf(time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC))},
			record.Field{Name: "paid", Value: true},
		),
		record.New(
			record.Field{Name: "id", Value: int64(8)},
			record.Field{Name: "customer", Value: `Schmidt "Ltd"`},
			record.Field{Name: "amount", Value: decimal.RequireFromString("-0.99")},
			record.Field{Name: "day", Value: nil},
			record.Field{Name: "paid", Value: false},
		),
	}
}

func write(t *testing.T, with map[string]any, rs ...record.Record) ([]record.Record, []transformtest.Logged, error) {
	t.Helper()
	return transformtest.Run(context.Background(), transformtest.New(t, outputDefinition, with), rs...)
}

// golden compares a written file with testdata/<name> byte by byte.
func golden(t *testing.T, path, name string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	g := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(g, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(g)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from %s:\n%q\nwant\n%q", path, g, got, want)
	}
}

func TestOutputAllFields(t *testing.T) {
	p := filepath.Join(t.TempDir(), "out.csv")
	out, logs, err := write(t, map[string]any{"path": p}, orders()...)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Errorf("passed on %d records, want 2", len(out))
	}
	if len(logs) != 1 || logs[0].Msg != "wrote 2 rows to "+p {
		t.Errorf("log %v", logs)
	}
	// UTF-8 with BOM, CRLF, quotes where needed, ISO dates, plain numbers.
	golden(t, p, "orders.csv")
}

func TestOutputFieldsAndFormats(t *testing.T) {
	p := filepath.Join(t.TempDir(), "out.csv")
	_, _, err := write(t, map[string]any{
		"path":      p,
		"delimiter": ";",
		"encoding":  "windows-1252",
		"fields": []any{
			map[string]any{"name": "day", "format": "dd.MM.yyyy"},
			map[string]any{"name": "customer"},
			map[string]any{"name": "amount", "format": "#.##0,00"},
			map[string]any{"name": "missing"},
		},
	}, orders()...)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, p, "orders-de-1252.csv")
}

func TestOutputRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "out.csv")
	if _, _, err := write(t, map[string]any{"path": p, "delimiter": ";",
		"fields": []any{map[string]any{"name": "amount", "format": "#.##0,00"}, map[string]any{"name": "day", "format": "dd.MM.yyyy"}},
	}, orders()...); err != nil {
		t.Fatal(err)
	}
	out, err := read(t, map[string]any{"path": p, "delimiter": ";", "fields": []any{
		map[string]any{"name": "amount", "type": "decimal", "format": "#.##0,00"},
		map[string]any{"name": "day", "type": "date", "format": "dd.MM.yyyy"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := "amount=1234.5(decimal.Decimal) day=2026-11-19(record.Date)\namount=-0.99(decimal.Decimal) day=<nil>(<nil>)\n"
	if got := show(out); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestOutputIfExists(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.csv")
	one := record.New(record.Field{Name: "a", Value: "1"})
	two := record.New(record.Field{Name: "a", Value: "2"})

	// append to a missing file writes the header and the BOM, later without both.
	for _, r := range []record.Record{one, two} {
		if _, _, err := write(t, map[string]any{"path": p, "ifExists": "append"}, r); err != nil {
			t.Fatal(err)
		}
	}
	golden(t, p, "append.csv")

	// fail leaves the file as it is.
	if _, _, err := write(t, map[string]any{"path": p, "ifExists": "fail"}, one); err == nil || !strings.Contains(err.Error(), "already exists (ifExists: fail)") {
		t.Errorf("fail: %v", err)
	}
	golden(t, p, "append.csv")

	// replace keeps the old file when writing fails, and leaves no temporary file.
	bad := record.New(record.Field{Name: "a", Value: []byte{1}})
	if _, _, err := write(t, map[string]any{"path": p}, one, bad); err == nil || !strings.Contains(err.Error(), `record 2, field "a": binary values cannot be written as text`) {
		t.Errorf("binary: %v", err)
	}
	golden(t, p, "append.csv")
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("files left: %v", entries)
	}

	if _, _, err := write(t, map[string]any{"path": p, "header": false, "encoding": "utf-8"}, two); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "2\r\n" {
		t.Errorf("replaced: %q", b)
	}
}

func TestOutputWithoutRecords(t *testing.T) {
	dir := t.TempDir()
	// The columns are known from fields or a closed schema: the file has its header.
	p := filepath.Join(dir, "fields.csv")
	if _, _, err := write(t, map[string]any{"path": p, "encoding": "utf-8", "fields": []any{map[string]any{"name": "a"}, map[string]any{"name": "b"}}}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "a,b\r\n" {
		t.Errorf("fields: %q", b)
	}
	p = filepath.Join(dir, "schema.csv")
	tr := transformtest.New(t, outputDefinition, map[string]any{"path": p, "encoding": "utf-8"})
	if _, err := tr.(transform.Initializer).Init(transform.NewInitContext(record.Schema{}.With("x", record.Int).With("y", record.String))); err != nil {
		t.Fatal(err)
	}
	if _, _, err := transformtest.Run(context.Background(), tr); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "x,y\r\n" {
		t.Errorf("schema: %q", b)
	}
	// Unknown columns: an empty file.
	p = filepath.Join(dir, "open.csv")
	if _, _, err := write(t, map[string]any{"path": p}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "\xEF\xBB\xBF" {
		t.Errorf("open: %q", b)
	}
}

func TestOutputColumnsFromFirstRecord(t *testing.T) {
	p := filepath.Join(t.TempDir(), "out.csv")
	_, logs, err := write(t, map[string]any{"path": p, "encoding": "utf-8"},
		record.New(record.Field{Name: "a", Value: "1"}),
		record.New(record.Field{Name: "a", Value: "2"}, record.Field{Name: "b", Value: "x"}),
		record.New(record.Field{Name: "b", Value: "y"}, record.Field{Name: "a", Value: "3"}))
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "a\r\n1\r\n2\r\n3\r\n" {
		t.Errorf("%q", b)
	}
	if len(logs) != 2 || !strings.Contains(logs[0].Msg, `the field "b" is not written`) {
		t.Errorf("log %v", logs)
	}
}

func TestOutputErrors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.csv")
	_, _, err := write(t, map[string]any{"path": p, "encoding": "windows-1252"}, record.New(record.Field{Name: "a", Value: "Ω"}))
	if err == nil || !strings.Contains(err.Error(), `record 1, field "a": the character 'Ω' cannot be written in windows-1252; use encoding: utf-8-bom`) {
		t.Errorf("windows-1252: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("file written despite the error: %v", err)
	}
	if _, _, err := write(t, map[string]any{"path": dir}); err == nil || !strings.Contains(err.Error(), "it is a folder") {
		t.Errorf("folder: %v", err)
	}
	if _, _, err := write(t, map[string]any{"path": filepath.Join(dir, "missing", "out.csv")}); err == nil || !strings.Contains(err.Error(), "cannot write") {
		t.Errorf("missing folder: %v", err)
	}
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"path": "x", "encoding": "utf-16"}, `invalid encoding "utf-16"`},
		{map[string]any{"path": "x", "ifExists": "skip"}, `invalid ifExists "skip"`},
		{map[string]any{"path": "x", "fields": []any{map[string]any{"name": "a", "format": "dd.QQ"}}}, `field "a"`},
	} {
		if _, err := outputDefinition.New(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: error %v, want %q", tt.with, err, tt.want)
		}
	}
}

func TestOutputSchema(t *testing.T) {
	tr := transformtest.New(t, outputDefinition, map[string]any{"path": "x", "fields": []any{map[string]any{"name": "amont"}}})
	ic := transform.NewInitContext(record.Schema{}.With("amount", record.Decimal))
	s, err := tr.(transform.Initializer).Init(ic)
	if err != nil || len(s.Names()) != 1 {
		t.Errorf("schema %+v %v, want the incoming one", s, err)
	}
	if p := ic.Problems(); len(p) != 1 || p[0].Path != "fields[0].name" || !strings.Contains(p[0].Message, `did you mean "amount"?`) {
		t.Errorf("problems %+v", p)
	}
}

func TestOutputErrorLink(t *testing.T) {
	p := filepath.Join(t.TempDir(), "out.csv")
	tr := transformtest.New(t, outputDefinition, map[string]any{"path": p, "encoding": "windows-1252"})
	out, failed, logs, err := transformtest.RunWithErrorLink(context.Background(), tr,
		record.New(record.Field{Name: "a", Value: "Jörg"}),
		record.New(record.Field{Name: "a", Value: "Ω"}),
		record.New(record.Field{Name: "a", Value: "Ada"}))
	if err != nil {
		t.Fatal(err)
	}
	// The record that cannot be written goes to the error link and is not passed on.
	if len(out) != 2 || len(failed) != 1 {
		t.Fatalf("out %d, failed %d", len(out), len(failed))
	}
	if f, _ := failed[0].Get("_errorField"); f != "a" {
		t.Errorf("_errorField %v", f)
	}
	if b, _ := os.ReadFile(p); string(b) != "a\r\nJ\xf6rg\r\nAda\r\n" {
		t.Errorf("file %q", b)
	}
	if len(logs) != 1 || logs[0].Msg != "wrote 2 rows to "+p {
		t.Errorf("log %v", logs)
	}
}
