package excel

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

// BenchmarkInput reads 10,000 rows of 5 columns into records: text, whole and decimal
// numbers, a date and true/false; once with the values of the cells, once with fields.
func BenchmarkInput(b *testing.B) {
	var sb strings.Builder
	sb.WriteString(`<row r="1"><c r="A1" t="inlineStr"><is><t>Name</t></is></c><c r="B1" t="inlineStr"><is><t>Nr</t></is></c><c r="C1" t="inlineStr"><is><t>Amount</t></is></c><c r="D1" t="inlineStr"><is><t>Date</t></is></c><c r="E1" t="inlineStr"><is><t>Paid</t></is></c></row>`)
	for i := 2; i <= 10001; i++ {
		fmt.Fprintf(&sb, `<row r="%d"><c r="A%d" t="inlineStr"><is><t>Customer %d</t></is></c><c r="B%d"><v>%d</v></c><c r="C%d"><v>%d.25</v></c><c r="D%d" s="1"><v>%d</v></c><c r="E%d" t="b"><v>1</v></c></row>`,
			i, i, i, i, i, i, i, i, 45000+i%1000, i)
	}
	path := workbook(b, sb.String())
	for name, with := range map[string]map[string]any{
		"cells":  {"path": path},
		"fields": {"path": path, "fields": []any{map[string]any{"name": "Nr", "type": "string"}, map[string]any{"name": "Amount", "type": "float"}, map[string]any{"name": "Date", "type": "datetime"}}},
	} {
		b.Run(name, func(b *testing.B) {
			tr, err := newInput(with)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				out, _, err := transformtest.Run(context.Background(), tr)
				if err != nil || len(out) != 10000 {
					b.Fatal(len(out), err)
				}
			}
		})
	}
}
