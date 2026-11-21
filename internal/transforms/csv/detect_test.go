package csv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/transform"
)

func writeCSV(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "in.csv")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// summary writes the fields of a detection as lines "name type format state selected | note".
func summary(d *transform.Detection) string {
	var b strings.Builder
	for _, f := range d.Fields {
		fmt.Fprintf(&b, "%s %s %q %q %v | %s", f.Name, f.Type, f.Format, f.State, f.Selected, strings.Join(f.Values, ","))
		if f.Note != "" {
			fmt.Fprintf(&b, " | %s", f.Note)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestDetectFields(t *testing.T) {
	path := writeCSV(t, "kdnr;name;umsatz;ort;telefon\r\n4711;Müller GmbH;12.500,00;Köln;0221 123456\r\n"+
		"4712;Schmidt & Söhne;0,00;Bonn;\r\n4713;\"Weber, Anna\";980,50;Aachen;0241 98765\r\n4714;Klein KG;k. A.;Essen;\r\n")
	d, err := detectFields(map[string]any{"path": path, "delimiter": ";"}, transform.DetectLimit)
	if err != nil {
		t.Fatal(err)
	}
	want := `kdnr int "" "" true | 4711,4712,4713,4714
name string "" "" true | Müller GmbH,Schmidt & Söhne,Weber, Anna,Klein KG
umsatz string "" "" true | 12.500,00,0,00,980,50,k. A. | 3 of 4 values are decimal (#.##0,00); “k. A.” in line 5 is not. Kept as text.
ort string "" "" true | Köln,Bonn,Aachen,Essen
telefon string "" "" true | 0221 123456,,0241 98765, | Leading zero: kept as text
`
	if got := summary(d); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if d.Rows != 4 || !d.Complete {
		t.Errorf("rows %d complete %v; want 4, true", d.Rows, d.Complete)
	}
	facts := fmt.Sprint(d.Source)
	if !strings.Contains(facts, "{Delimiter ;}") || !strings.Contains(facts, "{Encoding UTF-8}") {
		t.Errorf("source = %v", facts)
	}
}

// TestDetectFieldsListed checks detecting again: listed fields keep type and format,
// new columns are not selected, listed ones missing in the file are marked.
func TestDetectFieldsListed(t *testing.T) {
	path := writeCSV(t, "\xEF\xBB\xBFNr;Kunde;Betrag;Datum;Bezahlt\n1001;Müller GmbH;1.234,50;19.11.2026;ja\n1002;Weber;89,90;20.11.2026;nein\n")
	d, err := detectFields(map[string]any{"path": path, "delimiter": ";", "fields": []any{
		map[string]any{"name": "Nr", "type": "int"},
		map[string]any{"name": "Kunde"},
		map[string]any{"name": "Betrag", "type": "decimal", "format": "#.##0,00"},
		map[string]any{"name": "Region"},
	}}, transform.DetectLimit)
	if err != nil {
		t.Fatal(err)
	}
	want := `Nr int "" "listed" true | 1001,1002
Kunde string "" "listed" true | Müller GmbH,Weber
Betrag decimal "#.##0,00" "listed" true | 1.234,50,89,90
Region string "" "missing" true |  | Not in the input
Datum date "dd.MM.yyyy" "new" false | 19.11.2026,20.11.2026
Bezahlt string "" "new" false | ja,nein | ja/nein is not a bool (true/false, 1/0, yes/no): kept as text
`
	if got := summary(d); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(fmt.Sprint(d.Source), "UTF-8 (byte order mark)") {
		t.Errorf("source = %v", d.Source)
	}
}

func TestDetectFieldsNoHeader(t *testing.T) {
	path := writeCSV(t, "1,a\n2,b\n3,c\n")
	d, err := detectFields(map[string]any{"path": path, "header": false, "fields": []any{map[string]any{"name": "id", "type": "int"}}}, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := `id int "" "listed" true | 1,2
field2 string "" "new" false | a,b
`
	if got := summary(d); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if d.Rows != 2 || d.Complete {
		t.Errorf("rows %d complete %v; want 2 of more", d.Rows, d.Complete)
	}
}

func TestDetectFieldsErrors(t *testing.T) {
	dir := t.TempDir()
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"path": filepath.Join(dir, "missing.csv")}, "cannot read"},
		{map[string]any{"path": writeCSV(t, "a;a\n1;2\n"), "delimiter": ";"}, `the column "a" appears twice`},
		{map[string]any{"path": writeCSV(t, "a\n\"x\n")}, "extraneous or missing"},
	} {
		if _, err := detectFields(tt.with, 10); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("detectFields(%v) = %v; want %q", tt.with, err, tt.want)
		}
	}
}
