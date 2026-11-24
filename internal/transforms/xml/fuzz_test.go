package xml

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

// FuzzInput reads any file with any record path through xml.input and its field
// detection: no panic, every record has fields.
func FuzzInput(f *testing.F) {
	seed, _ := os.ReadFile(orders)
	f.Add(seed, "Bestellung", "Kunde/@id")
	f.Add([]byte(`<?xml version="1.0" encoding="windows-1252"?><a><b x="1">t<c/>u</b><b><![CDATA[x]]></b></a>`), "", "#text")
	f.Add([]byte("\xFF\xFE<\x00a\x00/\x00>\x00"), "/a", "@x")
	f.Fuzz(func(t *testing.T, data []byte, records, from string) {
		p := filepath.Join(t.TempDir(), "in.xml")
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		with := map[string]any{"path": p, "records": records, "fields": []any{map[string]any{"name": "v", "from": from}}}
		if n, err := newInput(with); err == nil {
			out, failed, _, err := transformtest.RunWithErrorLink(context.Background(), n)
			if err == nil {
				for _, r := range append(out, failed...) {
					if len(r.Names()) == 0 {
						t.Fatal("record without fields")
					}
				}
			}
		}
		delete(with, "fields")
		_, _ = detectFields(context.Background(), with, transform.DetectLimit)
	})
}
