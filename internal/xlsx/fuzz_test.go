package xlsx

import (
	"bytes"
	"testing"
)

// FuzzRows reads any sheet, shared strings and styles: no panic, and the rows come in
// order with columns in order.
func FuzzRows(f *testing.F) {
	f.Add(sheet(`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" s="1"><v>45658.5</v></c></row>`),
		`<sst><si><r><t>a</t></r><rPh><t>b</t></rPh></si></sst>`, `<styleSheet><numFmts><numFmt numFmtId="164" formatCode="[h]:mm"/></numFmts><cellXfs><xf/><xf numFmtId="164"/></cellXfs></styleSheet>`)
	f.Add(sheet(`<row><c t="inlineStr"><is><t>x_x000D_</t></is></c><c t="b"><v>1</v></c><c t="e"><v>#N/A</v></c><c t="d"><v>2026-01-02</v></c></row>`), `<sst/>`, `<styleSheet/>`)
	f.Fuzz(func(t *testing.T, rows, strings, styles string) {
		data := book(t, map[string]string{"xl/worksheets/sheet1.xml": rows, "xl/sharedStrings.xml": strings, "xl/styles.xml": styles})
		b, err := OpenReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return
		}
		r, err := b.Rows(b.Sheets()[0])
		if err != nil {
			return
		}
		defer func() { _ = r.Close() }()
		last := 0
		for r.Next() {
			n, cells := r.Row()
			if n <= last || n > maxRows {
				t.Fatalf("row %d after %d", n, last)
			}
			last = n
			for i, c := range cells {
				if c.Col < 1 || c.Col > maxColumns || (i > 0 && c.Col <= cells[i-1].Col) || (c.Value == nil && c.Error == "") {
					t.Fatalf("row %d: cell %+v", n, c)
				}
			}
		}
	})
}

// FuzzOpen opens any bytes as a workbook: no panic.
func FuzzOpen(f *testing.F) {
	f.Add(book(f, nil))
	f.Add([]byte("PK\x03\x04"))
	f.Fuzz(func(_ *testing.T, data []byte) {
		b, err := OpenReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return
		}
		for _, s := range b.Sheets() {
			if r, err := b.Rows(s); err == nil {
				for r.Next() {
				}
				_ = r.Close()
			}
		}
	})
}
