package xml

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/transform"
)

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
	d, err := detectFields(context.Background(), map[string]any{"path": orders}, transform.DetectLimit)
	if err != nil {
		t.Fatal(err)
	}
	want := `@nr int "" "" true | 1001,1002,1003
Kunde json "" "" true | (element),(element),(element) | element with child elements; read single values with from: Kunde/child
Betrag string "" "" true | 1234.50,89.90,k. A. | 2 of 3 values are decimal; “k. A.” is not. Kept as text.
Datum date "" "" true | 2026-11-19,2026-11-20,2026-11-21
Position json "" "" true | (2 elements),(element), | repeated element: a list; read single values with their own fields
Notiz string "" "" true | Bitte <vor> 12 Uhr liefern,,
`
	if got := summary(d); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if d.Rows != 3 || !d.Complete || fmt.Sprint(d.Source) != "[{File testdata/orders.xml} {Records the children of the root element}]" {
		t.Errorf("rows %d complete %v source %v", d.Rows, d.Complete, d.Source)
	}

	// Records deeper in the file: a hint; the limit; listed fields.
	p := filepath.Join(t.TempDir(), "deep.xml")
	if err := os.WriteFile(p, []byte("<export><head><date>2026-11-24</date></head><items><item><id>1</id></item><item><id>2</id></item><item><id>3</id></item></items></export>"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err = detectFields(context.Background(), map[string]any{"path": p}, 1)
	if err != nil || d.Complete || d.Rows != 1 || !strings.Contains(fmt.Sprint(d.Source), "{Hint <item> appears 3 times; to read it as records set records: export/items/item}") {
		t.Errorf("%v %v %d %v", err, d.Complete, d.Rows, d.Source)
	}
	d, err = detectFields(context.Background(), map[string]any{"path": p, "records": "item", "fields": []any{map[string]any{"name": "id", "type": "int"}, map[string]any{"name": "x"}}}, 10)
	if err != nil || summary(d) != "id int \"\" \"listed\" true | 1,2,3\nx string \"\" \"missing\" true |  | Not in the input\n" {
		t.Errorf("%v\n%s", err, summary(d))
	}
}
