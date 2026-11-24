package excel

import (
	"context"
	"testing"

	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

// FuzzInput reads any sheet with any range through excel.input and its field detection:
// no panic, and every record has the columns of the table.
func FuzzInput(f *testing.F) {
	f.Add(`<row r="2"><c r="B2" t="inlineStr"><is><t>a</t></is></c><c r="C2"><v>1</v></c></row><row r="4"><c r="B4" s="1"><v>46000</v></c><c r="D4" t="e"><v>#N/A</v></c></row>`, "B2", true, false)
	f.Add(`<row><c t="inlineStr"><is><t>x</t></is></c></row><row><c><v>1.5</v></c><c><v>2</v></c></row>`, "A1:A3", false, true)
	f.Fuzz(func(t *testing.T, rows, rng string, header, stop bool) {
		with := map[string]any{"path": workbook(t, rows), "range": rng, "header": header, "emptyRows": map[bool]string{true: "stop", false: "skip"}[stop]}
		n, err := newInput(with)
		if err != nil {
			return
		}
		out, failed, _, err := transformtest.RunWithErrorLink(context.Background(), n)
		if err == nil {
			for _, r := range append(out, failed...) {
				if len(r.Names()) == 0 {
					t.Fatal("record without fields")
				}
			}
		}
		_, _ = detectFields(context.Background(), with, transform.DetectLimit)
	})
}
