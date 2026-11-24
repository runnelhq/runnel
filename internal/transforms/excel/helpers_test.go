package excel

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
)

func decimalOf(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func dateOf(s string) record.Date {
	d, err := record.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

func timeOf() time.Time { return time.Date(2026, 11, 2, 8, 30, 0, 0, time.Local) }

// workbook writes a workbook with one sheet (the XML inside <sheetData>) and returns its
// path; texts are inline, s="1" is a date.
func workbook(t testing.TB, rows string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "book.xlsx")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range map[string]string{
		"_rels/.rels":                `<Relationships><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml":            `<workbook xmlns:r="r"><sheets><sheet name="Data" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Type="x/worksheet" Target="sheet.xml"/><Relationship Id="rId2" Type="x/styles" Target="styles.xml"/></Relationships>`,
		"xl/styles.xml":              `<styleSheet><cellXfs><xf numFmtId="0"/><xf numFmtId="14"/></cellXfs></styleSheet>`,
		"xl/sheet.xml":               `<worksheet><sheetData>` + rows + `</sheetData></worksheet>`,
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}
