package xml

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

// BenchmarkInput reads 10,000 records with an attribute, a nested element and three
// fields, once as read and once with typed fields.
func BenchmarkInput(b *testing.B) {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?><orders>`)
	for i := range 10000 {
		fmt.Fprintf(&sb, `<order id="%d"><customer><name>Customer %d</name><city>Köln</city></customer><amount>%d.25</amount><date>2026-11-%02d</date></order>`, i, i, i, i%28+1)
	}
	sb.WriteString(`</orders>`)
	p := filepath.Join(b.TempDir(), "orders.xml")
	if err := os.WriteFile(p, []byte(sb.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	for name, with := range map[string]map[string]any{
		"asRead": {"path": p},
		"fields": {"path": p, "records": "order", "fields": []any{map[string]any{"name": "id", "from": "@id", "type": "int"},
			map[string]any{"name": "name", "from": "customer/name"}, map[string]any{"name": "amount", "type": "decimal"}, map[string]any{"name": "date", "type": "date"}}},
	} {
		b.Run(name, func(b *testing.B) {
			tr, err := newInput(with)
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(sb.Len()))
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
