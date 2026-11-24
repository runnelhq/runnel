package xml

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

// testdata/orders.xml: three orders with a namespace prefix, attributes, a nested
// customer, an amount with an attribute, repeated positions, CDATA and an entity; the
// amount of order 1003 is "k. A.".
const orders = "testdata/orders.xml"

// show writes records as one line each, with the Go type of every value; objects with
// their keys sorted.
func show(rs []record.Record) string {
	var b strings.Builder
	for _, r := range rs {
		for i, name := range r.Names() {
			v, _ := r.Get(name)
			if i > 0 {
				b.WriteString(" ")
			}
			fmt.Fprintf(&b, "%s=%s(%T)", name, text(v), v)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func text(v any) string {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + ":" + text(x[k])
		}
		return "{" + strings.Join(parts, " ") + "}"
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = text(e)
		}
		return "[" + strings.Join(parts, " ") + "]"
	}
	return fmt.Sprint(v)
}

func run(t *testing.T, with map[string]any) (out, failed []record.Record, logs []transformtest.Logged, err error) {
	t.Helper()
	return transformtest.RunWithErrorLink(context.Background(), transformtest.New(t, inputDefinition, with))
}

// Without fields: attributes as @name, elements with text as text, others as objects,
// repeated ones as lists; namespace prefixes are dropped.
func TestInputAsRead(t *testing.T) {
	out, _, logs, err := run(t, map[string]any{"path": orders})
	if err != nil {
		t.Fatal(err)
	}
	want := `@nr=1001(string) Kunde={@id:K-17 Name:Müller & Söhne Ort:Köln}(map[string]interface {}) Betrag={#text:1234.50 @waehrung:EUR}(map[string]interface {}) Datum=2026-11-19(string) Position=[{@artikel:A1 @menge:2} {@artikel:B7 @menge:1}]([]interface {}) Notiz=Bitte <vor> 12 Uhr liefern(string)
@nr=1002(string) Kunde={@id:K-3 Name:Weber, Anna Ort:Bonn}(map[string]interface {}) Betrag={#text:89.90 @waehrung:EUR}(map[string]interface {}) Datum=2026-11-20(string) Position={@artikel:C2 @menge:5}(map[string]interface {})
@nr=1003(string) Kunde={@id:K-9 Name:Klein KG}(map[string]interface {}) Betrag={#text:k. A. @waehrung:EUR}(map[string]interface {}) Datum=2026-11-21(string)
`
	if got := show(out); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if len(logs) != 1 || logs[0].Msg != "read 3 records from testdata/orders.xml" {
		t.Errorf("logs %v", logs)
	}
}

// Fields with paths, attributes and types; an amount that is no number goes to the
// error link with the line of its record.
func TestInputFields(t *testing.T) {
	out, failed, logs, err := run(t, map[string]any{"path": orders, "records": "Bestellungen/Bestellung", "fields": []any{
		map[string]any{"name": "Nr", "from": "@nr", "type": "int"},
		map[string]any{"name": "Kunde", "from": "Kunde/Name"},
		map[string]any{"name": "KundeId", "from": "b:Kunde/@id"},
		map[string]any{"name": "Betrag", "type": "decimal"},
		map[string]any{"name": "Waehrung", "from": "Betrag/@waehrung"},
		map[string]any{"name": "Datum", "type": "date"},
		map[string]any{"name": "Positionen", "from": "Position", "type": "json"},
		map[string]any{"name": "Fehlt"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := `Nr=1001(int64) Kunde=Müller & Söhne(string) KundeId=K-17(string) Betrag=1234.5(decimal.Decimal) Waehrung=EUR(string) Datum=2026-11-19(record.Date) Positionen=[{@artikel:A1 @menge:2} {@artikel:B7 @menge:1}]([]interface {}) Fehlt=<nil>(<nil>)
Nr=1002(int64) Kunde=Weber, Anna(string) KundeId=K-3(string) Betrag=89.9(decimal.Decimal) Waehrung=EUR(string) Datum=2026-11-20(record.Date) Positionen={@artikel:C2 @menge:5}(map[string]interface {}) Fehlt=<nil>(<nil>)
`
	if got := show(out); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if len(failed) != 1 {
		t.Fatalf("failed %s", show(failed))
	}
	if e, _ := failed[0].Get("_error"); !strings.HasPrefix(fmt.Sprint(e), `testdata/orders.xml:24: record 3, field "Betrag": `) {
		t.Errorf("error %v", e)
	}
	if len(logs) != 1 || logs[0].Msg != "read 3 records from testdata/orders.xml, 1 with errors" {
		t.Errorf("logs %v", logs)
	}
}

// records matches the end of the path of an element, / at the start the root; elements
// with children as typed values and repeated elements give clear errors.
func TestInputRecordsAndErrors(t *testing.T) {
	out, _, _, err := run(t, map[string]any{"path": orders, "records": "Kunde", "fields": []any{map[string]any{"name": "Name"}, map[string]any{"name": "Ort"}}})
	if err != nil || show(out) != "Name=Müller & Söhne(string) Ort=Köln(string)\nName=Weber, Anna(string) Ort=Bonn(string)\nName=Klein KG(string) Ort=<nil>(<nil>)\n" {
		t.Errorf("Kunde: %s %v", show(out), err)
	}
	if out, _, _, err := run(t, map[string]any{"path": orders, "records": "/Kunde"}); err != nil || len(out) != 0 {
		t.Errorf("/Kunde: %d %v", len(out), err)
	}
	if out, _, _, err := run(t, map[string]any{"path": orders, "records": "/Bestellungen"}); err != nil || len(out) != 1 {
		t.Errorf("/Bestellungen: %d %v", len(out), err)
	}
	_, failed, _, err := run(t, map[string]any{"path": orders, "fields": []any{
		map[string]any{"name": "Kunde", "type": "string"},
	}})
	if err != nil || len(failed) != 3 || !strings.Contains(fmt.Sprint(failed[0].Get("_error")), "the element has child elements, not a single value") {
		t.Errorf("object: %s %v", show(failed), err)
	}
	_, failed, _, err = run(t, map[string]any{"path": orders, "fields": []any{map[string]any{"name": "a", "from": "Position/@artikel"}}})
	if err != nil || len(failed) != 1 || !strings.Contains(fmt.Sprint(failed[0].Get("_error")), "<Position> appears 2 times; read it as a list with type json") {
		t.Errorf("list: %s %v", show(failed), err)
	}

	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"path": filepath.Join(dir, "none.xml")}, "none.xml\": "},
		{map[string]any{"path": write("broken.xml", "<a>\n<b>1</c>\n</a>")}, "broken.xml:2: XML error: element <b> closed by </c>"},
		{map[string]any{"path": write("cut.xml", "<a>\n<b>1</b>\n<b>2")}, "cut.xml:3: XML error: unexpected EOF"},
		{map[string]any{"path": write("enc.xml", `<?xml version="1.0" encoding="Shift_JIS"?><a/>`)}, `the encoding "Shift_JIS" is not supported`},
		{map[string]any{"path": write("utf16.xml", `<?xml version="1.0" encoding="UTF-16"?><a/>`)}, "declares UTF-16 but has no byte order mark"},
		{map[string]any{"path": write("entity.xml", `<!DOCTYPE a [<!ENTITY x SYSTEM "file:///etc/passwd">]><a><b>&x;</b></a>`)}, "XML error: invalid character entity &x;"},
		{map[string]any{"path": write("deep.xml", "<a><b>"+strings.Repeat("<c>", maxDepth+1)+strings.Repeat("</c>", maxDepth+1)+"</b></a>")}, "nested more than 256 deep"},
	} {
		_, _, _, err := run(t, tt.with)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with["path"], err, tt.want)
		}
	}
	// An empty file has no records.
	if out, _, _, err := run(t, map[string]any{"path": write("empty.xml", "")}); err != nil || len(out) != 0 {
		t.Errorf("empty: %d %v", len(out), err)
	}

	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"path": "a.xml", "records": "orders[1]/order"}, `records: "orders[1]/order" is not a path of element names`},
		{map[string]any{"path": "a.xml", "fields": []any{map[string]any{"name": "a", "from": "@id/x"}}}, `field "a": from: "@id/x": an attribute or #text can only be the last part`},
		{map[string]any{"path": "a.xml", "fields": []any{map[string]any{"name": "a", "from": "x[@id=1]"}}}, `is not a path like customer/name or @id`},
		{map[string]any{"path": "a.xml", "fields": []any{map[string]any{"name": "a"}, map[string]any{"name": "a"}}}, `the field "a" is listed twice`},
		{map[string]any{"path": "a.xml", "fields": []any{map[string]any{"name": "a", "type": "money"}}}, `field "a": `},
	} {
		if _, err := newInput(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
}

// The encoding comes from a byte order mark (UTF-16, UTF-8) or the declaration.
func TestInputEncodings(t *testing.T) {
	dir := t.TempDir()
	doc := `<?xml version="1.0" encoding="%s"?><orders><order><name>Müller</name><city>Köln</city></order></orders>`
	utf16, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().String(fmt.Sprintf(doc, "UTF-16"))
	if err != nil {
		t.Fatal(err)
	}
	cp1252, err := charmap.Windows1252.NewEncoder().String(fmt.Sprintf(doc, "windows-1252"))
	if err != nil {
		t.Fatal(err)
	}
	latin1, err := charmap.ISO8859_1.NewEncoder().String(fmt.Sprintf(doc, "ISO-8859-1"))
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"utf8.xml": fmt.Sprintf(doc, "UTF-8"), "bom.xml": "\xEF\xBB\xBF" + fmt.Sprintf(doc, "utf-8"),
		"utf16.xml": utf16, "cp1252.xml": cp1252, "latin1.xml": latin1,
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		out, _, _, err := run(t, map[string]any{"path": p})
		if got := show(out); err != nil || got != "name=Müller(string) city=Köln(string)\n" {
			t.Errorf("%s: %q %v", name, got, err)
		}
	}
}

func TestInit(t *testing.T) {
	tr, err := newInput(map[string]any{"path": orders, "fields": []any{map[string]any{"name": "Nr", "from": "@nr", "type": "int"}, map[string]any{"name": "Kunde"}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := tr.(transform.Initializer).Init(transform.NewInitContext(record.Schema{}))
	if err != nil || s.Open || fmt.Sprint(s.Names()) != "[Nr Kunde]" {
		t.Fatalf("%v %v", s, err)
	}
	if f, _ := s.Field("Kunde"); f.Type != record.Unknown {
		t.Errorf("Kunde: %v", f.Type)
	}
}
