package format

import (
	"testing"

	"github.com/runnelhq/runnel/internal/record"
)

var fuzzTypes = []record.Type{record.String, record.Int, record.Decimal, record.Float, record.Bool, record.DateType, record.DateTime}

// FuzzParse checks that arbitrary patterns and texts (e.g. from CSV files) never make
// reading or writing a value panic, and that a value that was read can be written.
func FuzzParse(f *testing.F) {
	for _, s := range []struct {
		typ           int
		pattern, text string
	}{
		{1, "", "4711"}, {2, "#.##0,00", "1.234,50"}, {2, "#,##0.00", "1,234.50"}, {3, "", "1e3"},
		{4, "ja|nein", "ja"}, {5, "dd.MM.yyyy", "19.11.2026"}, {6, "yyyy-MM-dd HH:mm:ss", "2026-11-19 14:20:00"},
	} {
		f.Add(s.typ, s.pattern, s.text)
	}
	f.Fuzz(func(t *testing.T, typ int, pattern, text string) {
		if typ < 0 {
			typ = -typ
		}
		c, err := New(fuzzTypes[typ%len(fuzzTypes)], pattern)
		if err != nil {
			return
		}
		v, err := c.Parse(text)
		if err != nil {
			return
		}
		if _, err := c.Format(v); err != nil {
			t.Errorf("%s %q: %q read as %v (%T), but cannot be written: %v", c.Type(), pattern, text, v, v, err)
		}
	})
}
