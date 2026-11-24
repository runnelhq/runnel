package excel

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/transform"
)

// fields[].as names the column in the header; fields[].from reads it back (ADR 0025).
func TestFromAndAs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.xlsx")
	if _, _, err := write(t, map[string]any{"path": path, "fields": []any{
		map[string]any{"name": "Nr", "as": "Bestellnummer"},
		map[string]any{"name": "Kunde"},
	}}, orderRecords()...); err != nil {
		t.Fatal(err)
	}
	if got := readBack(t, path); !strings.HasPrefix(got, "[Sheet1]\n1: A=Bestellnummer(string) B=Kunde(string)\n2: A=1001(int64)") {
		t.Errorf("got\n%s", got)
	}
	// The width of the column follows the header that is written.
	if sheet := part(t, path, "xl/worksheets/sheet1.xml"); !strings.Contains(sheet, `<col min="1" max="1" width="16"`) {
		t.Errorf("width:\n%s", sheet)
	}
	out, _, _, err := run(t, map[string]any{"path": path, "fields": []any{
		map[string]any{"name": "id", "from": "Bestellnummer", "type": "string"},
		map[string]any{"name": "Kunde"},
	}})
	if err != nil || !strings.HasPrefix(show(out), "id=1001(string) Kunde=Müller & Söhne(string)\n") {
		t.Errorf("read back: %s %v", show(out), err)
	}
	if _, _, _, err := run(t, map[string]any{"path": path, "fields": []any{map[string]any{"name": "id", "from": "Nr"}}}); err == nil ||
		!strings.Contains(err.Error(), `the header has no column "Nr"`) {
		t.Errorf("missing: %v", err)
	}
	for _, tt := range []struct {
		def  transform.Definition
		with map[string]any
		want string
	}{
		{inputDefinition, map[string]any{"path": "a.xlsx", "header": false, "fields": []any{map[string]any{"name": "id", "from": "Nr"}}},
			`fields: field "id": from needs a header`},
		{outputDefinition, map[string]any{"path": "a.xlsx", "fields": []any{map[string]any{"name": "a", "as": "x"}, map[string]any{"name": "b", "as": "x"}}},
			`fields: field "b": the column "x" is written twice; choose another as`},
	} {
		if _, err := tt.def.New(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
}
