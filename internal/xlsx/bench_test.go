package xlsx

import (
	"fmt"
	"strings"
	"testing"
)

// BenchmarkRows reads a sheet with 10,000 rows of 6 cells: shared text, inline text,
// whole and decimal numbers, a date and true/false.
func BenchmarkRows(b *testing.B) {
	var sb strings.Builder
	for i := 1; i <= 10000; i++ {
		fmt.Fprintf(&sb, `<row r="%d"><c r="A%d" t="s"><v>0</v></c><c r="B%d"><v>%d</v></c><c r="C%d"><v>%d.25</v></c><c r="D%d" s="1"><v>%d</v></c><c r="E%d" t="b"><v>1</v></c><c r="F%d" t="inlineStr"><is><t>Text %d</t></is></c></row>`,
			i, i, i, i, i, i, i, 45000+i%1000, i, i, i)
	}
	data := book(b, map[string]string{
		"xl/worksheets/sheet1.xml": sheet(sb.String()),
		"xl/styles.xml":            `<styleSheet><cellXfs><xf numFmtId="0"/><xf numFmtId="14"/></cellXfs></styleSheet>`,
	})
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		book, err := open(b, data)
		if err != nil {
			b.Fatal(err)
		}
		r, err := book.Rows(book.Sheets()[0])
		if err != nil {
			b.Fatal(err)
		}
		n := 0
		for r.Next() {
			n++
		}
		if r.Err() != nil || n != 10000 {
			b.Fatal(n, r.Err())
		}
		_ = r.Close()
	}
}
