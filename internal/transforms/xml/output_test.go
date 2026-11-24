package xml

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

func orderRecords() []record.Record {
	day, _ := record.ParseDate("2026-11-19")
	return []record.Record{
		record.New(record.Field{Name: "Nr", Value: int64(1001)}, record.Field{Name: "Kunde", Value: "Müller & Söhne <GmbH>"},
			record.Field{Name: "KundenNr", Value: "K-17"}, record.Field{Name: "Betrag", Value: decimal.RequireFromString("1234.50")},
			record.Field{Name: "Datum", Value: day}, record.Field{Name: "Am", Value: time.Date(2026, 11, 19, 14, 5, 0, 0, time.UTC)},
			record.Field{Name: "Bezahlt", Value: true}, record.Field{Name: "Notiz", Value: nil},
			record.Field{Name: "Positionen", Value: []any{map[string]any{"@artikel": "A1", "menge": int64(2)}, map[string]any{"@artikel": "B7", "menge": int64(1)}}}),
		record.New(record.Field{Name: "Nr", Value: int64(1002)}, record.Field{Name: "Kunde", Value: "Weber, \"Anna\""},
			record.Field{Name: "KundenNr", Value: "K-3"}, record.Field{Name: "Betrag", Value: 89.9},
			record.Field{Name: "Datum", Value: nil}, record.Field{Name: "Am", Value: nil},
			record.Field{Name: "Bezahlt", Value: false}, record.Field{Name: "Notiz", Value: "Zeile 1\nZeile 2"},
			record.Field{Name: "Positionen", Value: nil}),
	}
}

func write(t *testing.T, with map[string]any, records ...record.Record) ([]record.Record, []transformtest.Logged, error) {
	t.Helper()
	return transformtest.Run(context.Background(), transformtest.New(t, outputDefinition, with), records...)
}

// Without fields: every field a child element, lists repeated, objects nested with
// their attributes; empty values left out; text escaped.
func TestOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.xml")
	out, logs, err := write(t, map[string]any{"path": path, "root": "Bestellungen", "record": "Bestellung"}, orderRecords()...)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || len(logs) != 1 || logs[0].Msg != "wrote 2 records to "+path {
		t.Errorf("%d %v", len(out), logs)
	}
	b, _ := os.ReadFile(path)
	want := `<?xml version="1.0" encoding="UTF-8"?>
<Bestellungen>
  <Bestellung>
    <Nr>1001</Nr>
    <Kunde>Müller &amp; Söhne &lt;GmbH&gt;</Kunde>
    <KundenNr>K-17</KundenNr>
    <Betrag>1234.5</Betrag>
    <Datum>2026-11-19</Datum>
    <Am>2026-11-19T14:05:00Z</Am>
    <Bezahlt>true</Bezahlt>
    <Positionen artikel="A1">
      <menge>2</menge>
    </Positionen>
    <Positionen artikel="B7">
      <menge>1</menge>
    </Positionen>
  </Bestellung>
  <Bestellung>
    <Nr>1002</Nr>
    <Kunde>Weber, &#34;Anna&#34;</Kunde>
    <KundenNr>K-3</KundenNr>
    <Betrag>89.9</Betrag>
    <Bezahlt>false</Bezahlt>
    <Notiz>Zeile 1&#xA;Zeile 2</Notiz>
  </Bestellung>
</Bestellungen>
`
	if string(b) != want {
		t.Errorf("got\n%s\nwant\n%s", b, want)
	}
}

// Fields choose the order and place: attributes, nested elements sharing one parent,
// #text, formats; nulls: empty writes empty elements. xml.input reads it back.
func TestOutputFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.xml")
	_, _, err := write(t, map[string]any{"path": path, "root": "orders", "record": "order", "nulls": "empty", "fields": []any{
		map[string]any{"name": "Nr", "as": "@nr"},
		map[string]any{"name": "Kunde", "as": "customer/name"},
		map[string]any{"name": "KundenNr", "as": "customer/@id"},
		map[string]any{"name": "Betrag", "as": "amount/#text", "format": "#.##0,00"},
		map[string]any{"name": "Datum", "as": "date", "format": "dd.MM.yyyy"},
	}}, orderRecords()...)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	want := `<?xml version="1.0" encoding="UTF-8"?>
<orders>
  <order nr="1001">
    <customer id="K-17">
      <name>Müller &amp; Söhne &lt;GmbH&gt;</name>
    </customer>
    <amount>1.234,50</amount>
    <date>19.11.2026</date>
  </order>
  <order nr="1002">
    <customer id="K-3">
      <name>Weber, &#34;Anna&#34;</name>
    </customer>
    <amount>89,90</amount>
    <date/>
  </order>
</orders>
`
	if string(b) != want {
		t.Errorf("got\n%s\nwant\n%s", b, want)
	}
	recs, _, _, err := run(t, map[string]any{"path": path, "fields": []any{
		map[string]any{"name": "nr", "from": "@nr", "type": "int"}, map[string]any{"name": "name", "from": "customer/name"},
		map[string]any{"name": "date", "type": "date", "format": "dd.MM.yyyy"},
	}})
	if got := show(recs); err != nil || got != "nr=1001(int64) name=Müller & Söhne <GmbH>(string) date=2026-11-19(record.Date)\nnr=1002(int64) name=Weber, \"Anna\"(string) date=<nil>(<nil>)\n" {
		t.Errorf("read back: %s %v", got, err)
	}
}

// Values XML cannot hold go to the error link; the file is complete or not written.
func TestOutputErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.xml")
	tr := transformtest.New(t, outputDefinition, map[string]any{"path": path})
	out, failed, _, err := transformtest.RunWithErrorLink(context.Background(), tr,
		record.New(record.Field{Name: "v", Value: "ok"}),
		record.New(record.Field{Name: "v", Value: "bell\a"}),
		record.New(record.Field{Name: "Order Nr", Value: "1"}),
		record.New(record.Field{Name: "v", Value: math.NaN()}),
		record.New(record.Field{Name: "v", Value: map[string]any{"bad name": "x"}}),
	)
	if err != nil || len(out) != 1 || len(failed) != 4 {
		t.Fatalf("%d %d %v", len(out), len(failed), err)
	}
	for i, want := range []string{
		`record 2, field "v": the text contains the character U+0007, which XML cannot hold`,
		`record 3, field "Order Nr": "Order Nr" is not a valid XML name`,
		`record 4, field "v": NaN cannot be written in XML`,
		`record 5, field "v": "bad name" is not a valid XML name`,
	} {
		if e, _ := failed[i].Get("_error"); !strings.Contains(fmt.Sprint(e), want) {
			t.Errorf("%d: %v", i, e)
		}
	}
	tr = transformtest.New(t, outputDefinition, map[string]any{"path": path, "fields": []any{
		map[string]any{"name": "a", "as": "@x"}, map[string]any{"name": "b", "as": "@x"}, map[string]any{"name": "c", "as": "@y"},
	}})
	_, failed, _, err = transformtest.RunWithErrorLink(context.Background(), tr,
		record.New(record.Field{Name: "a", Value: "1"}, record.Field{Name: "b", Value: "2"}),
		record.New(record.Field{Name: "c", Value: []any{"x"}}))
	if err != nil || len(failed) != 2 || !strings.Contains(fmt.Sprint(failed[0].Get("_error")), "the attribute @x of <record> is written twice") ||
		!strings.Contains(fmt.Sprint(failed[1].Get("_error")), "a list or object cannot be an attribute") {
		t.Errorf("attributes: %s %v", show(failed), err)
	}

	// Without an error link the pipeline stops and the old file stays; fail keeps it too.
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := write(t, map[string]any{"path": path}, record.New(record.Field{Name: "v", Value: math.Inf(1)})); err == nil || !strings.Contains(err.Error(), "+Inf cannot be written in XML") {
		t.Errorf("Inf: %v", err)
	}
	if _, _, err := write(t, map[string]any{"path": path, "ifExists": "fail"}, record.New()); err == nil || !strings.Contains(err.Error(), "already exists (ifExists: fail)") {
		t.Errorf("fail: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "old" {
		t.Errorf("file replaced: %q", b)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("files: %v", entries)
	}
	// No records: an empty root.
	if _, _, err := write(t, map[string]any{"path": path}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<records>\n</records>\n" {
		t.Errorf("empty: %q", b)
	}

	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"path": "a.xml", "root": "1orders"}, `root: "1orders" is not a valid XML name`},
		{map[string]any{"path": "a.xml", "record": "erp:order"}, `record: "erp:order" has a namespace prefix, which xml.output does not write yet`},
		{map[string]any{"path": "a.xml", "record": "xmlData"}, `"xmlData" is not a valid XML name`},
		{map[string]any{"path": "a.xml", "fields": []any{map[string]any{"name": "a", "as": "@id/x"}}}, `field "a": as: "@id/x": an attribute or #text can only be the last part`},
		{map[string]any{"path": "a.xml", "fields": []any{map[string]any{"name": "a", "as": "a b"}}}, `field "a": as: "a b" is not a valid XML name`},
		{map[string]any{"path": "a.xml", "ifExists": "append"}, `invalid ifExists "append" (expected replace or fail)`},
		{map[string]any{"path": "a.xml", "nulls": "nil"}, `invalid nulls "nil" (expected skip or empty)`},
		{map[string]any{"path": "a.xml", "fields": []any{map[string]any{"name": "a"}, map[string]any{"name": "a"}}}, `the field "a" is listed twice`},
	} {
		if _, err := newOutput(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
}

// BenchmarkOutput writes 10,000 records of 5 fields, one as attribute, one nested.
func BenchmarkOutput(b *testing.B) {
	rows := make([]record.Record, 10000)
	day, _ := record.ParseDate("2026-11-23")
	for i := range rows {
		rows[i] = record.New(record.Field{Name: "id", Value: int64(i)}, record.Field{Name: "name", Value: fmt.Sprintf("Customer %d", i)},
			record.Field{Name: "city", Value: "Köln"}, record.Field{Name: "amount", Value: decimal.RequireFromString("1234.25")}, record.Field{Name: "date", Value: day})
	}
	path := filepath.Join(b.TempDir(), "bench.xml")
	tr, err := newOutput(map[string]any{"path": path, "root": "orders", "record": "order", "fields": []any{
		map[string]any{"name": "id", "as": "@id"}, map[string]any{"name": "name", "as": "customer/name"}, map[string]any{"name": "city", "as": "customer/city"},
		map[string]any{"name": "amount"}, map[string]any{"name": "date"}}})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if out, _, err := transformtest.Run(context.Background(), tr, rows...); err != nil || len(out) != 10000 {
			b.Fatal(len(out), err)
		}
	}
}
