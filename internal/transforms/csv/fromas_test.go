package csv

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// fields[].from reads a column of another name; fields[].as writes one (ADR 0025).
func TestFromAndAs(t *testing.T) {
	p := writeFile(t, "Kunden-Nr;Name;Betrag\n7;Müller;1.234,50\n")
	out, err := read(t, map[string]any{"path": p, "delimiter": ";", "fields": []any{
		map[string]any{"name": "id", "from": "Kunden-Nr", "type": "int"},
		map[string]any{"name": "Name"},
		map[string]any{"name": "amount", "from": "Betrag", "type": "decimal", "format": "#.##0,00"},
	}})
	if want := "id=7(int64) Name=Müller(string) amount=1234.5(decimal.Decimal)\n"; err != nil || show(out) != want {
		t.Fatalf("read: %s %v", show(out), err)
	}

	q := filepath.Join(t.TempDir(), "out.csv")
	if _, _, err := write(t, map[string]any{"path": q, "delimiter": ";", "encoding": "utf-8", "fields": []any{
		map[string]any{"name": "id", "as": "Kunden-Nr"},
		map[string]any{"name": "Name"},
		map[string]any{"name": "amount", "as": "Betrag", "format": "#.##0,00"},
	}}, out...); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(q); string(b) != "Kunden-Nr;Name;Betrag\r\n7;Müller;1.234,50\r\n" {
		t.Errorf("written: %q", b)
	}

	// A missing column names from; from without header is an error.
	if _, err := read(t, map[string]any{"path": p, "delimiter": ";", "fields": []any{map[string]any{"name": "id", "from": "Kunde"}}}); err == nil ||
		!strings.Contains(err.Error(), `the header has no column "Kunde"`) {
		t.Errorf("missing: %v", err)
	}
	for _, tt := range []struct {
		def  transform.Definition
		with map[string]any
		want string
	}{
		{inputDefinition, map[string]any{"path": "a.csv", "header": false, "fields": []any{map[string]any{"name": "id", "from": "Nr"}}},
			`fields: field "id": from needs a header`},
		{outputDefinition, map[string]any{"path": "a.csv", "fields": []any{map[string]any{"name": "a", "as": "x"}, map[string]any{"name": "x"}}},
			`fields: field "x": the column "x" is written twice; choose another as`},
	} {
		if _, err := tt.def.New(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
}

// utf-16 writes little endian with byte order mark; csv.input reads it back.
func TestOutputUTF16(t *testing.T) {
	p := filepath.Join(t.TempDir(), "out.csv")
	r := record.New(record.Field{Name: "Name", Value: "Müller €"})
	if _, _, err := write(t, map[string]any{"path": p, "encoding": "utf-16"}, r); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != string(utf16(true, "Name\r\nMüller €\r\n")) {
		t.Errorf("written: % x", b)
	}
	out, err := read(t, map[string]any{"path": p})
	if err != nil || show(out) != "Name=Müller €(string)\n" {
		t.Errorf("read back: %s %v", show(out), err)
	}
	// Appending adds no second byte order mark.
	if _, _, err := write(t, map[string]any{"path": p, "encoding": "utf-16", "ifExists": "append"}, r); err != nil {
		t.Fatal(err)
	}
	if out, err := read(t, map[string]any{"path": p}); err != nil || show(out) != "Name=Müller €(string)\nName=Müller €(string)\n" {
		t.Errorf("appended: %s %v", show(out), err)
	}
}

// Detect fields matches a listed field with from by its column and keeps from.
func TestDetectFieldsFrom(t *testing.T) {
	path := writeCSV(t, "Kunden-Nr;Name\n7;Müller\n")
	d, err := detectFields(context.Background(), map[string]any{"path": path, "delimiter": ";", "fields": []any{
		map[string]any{"name": "id", "from": "Kunden-Nr", "type": "int"},
	}}, transform.DetectLimit)
	if err != nil {
		t.Fatal(err)
	}
	if f := d.Fields[0]; f.Name != "id" || f.From != "Kunden-Nr" || f.State != transform.FieldListed || len(d.Fields) != 2 || d.Fields[1].Name != "Name" {
		t.Errorf("fields = %+v", d.Fields)
	}
}
