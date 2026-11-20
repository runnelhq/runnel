package csv

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
)

func TestNumberFormats(t *testing.T) {
	for _, tt := range []struct {
		pattern, group, decimal string
		decimals                int
	}{
		{"#.##0,00", ".", ",", 2},
		{"#,##0.00", ",", ".", 2},
		{"#,##0", ",", ".", 0},
		{"#.##0", ".", ",", 0},
		{"0,00", "", ",", 2},
		{"0.000", "", ".", 3},
		{"#'##0.00", "'", ".", 2},
		{"# ##0,0", " ", ",", 1},
		{"#.###.##0", ".", ",", 0},
	} {
		n, err := parseNumberFormat(tt.pattern)
		if err != nil || n.group != tt.group || n.decimal != tt.decimal || n.decimals != tt.decimals {
			t.Errorf("%q: %+v %v", tt.pattern, n, err)
		}
	}
	for _, bad := range []string{"#.##0,00.0", "0,00x", "#'##0 000.0"} {
		if _, err := parseNumberFormat(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestCodecRoundTrip(t *testing.T) {
	day := record.Date{Year: 2026, Month: time.November, Day: 20}
	for _, tt := range []struct {
		f       field
		text    string
		value   any
		written string
	}{
		{field{Name: "s"}, "Grüße", "Grüße", "Grüße"},
		{field{Name: "s"}, "", "", ""},
		{field{Name: "i", Type: "int"}, "1234", int64(1234), "1234"},
		{field{Name: "i", Type: "int", Format: "#.##0"}, "1.234.567", int64(1234567), "1.234.567"},
		{field{Name: "i", Type: "int"}, "", nil, ""},
		{field{Name: "d", Type: "decimal", Format: "#.##0,00"}, "1.234,5", decimal.RequireFromString("1234.5"), "1.234,50"},
		{field{Name: "d", Type: "decimal"}, "-19.90", decimal.RequireFromString("-19.9"), "-19.9"},
		{field{Name: "d", Type: "decimal", Format: "0,00"}, "-1234,56", decimal.RequireFromString("-1234.56"), "-1234,56"},
		{field{Name: "f", Type: "float", Format: "#,##0.00"}, "1,000.25", 1000.25, "1,000.25"},
		{field{Name: "f", Type: "float"}, "0.1", 0.1, "0.1"},
		{field{Name: "b", Type: "bool"}, "yes", true, "true"},
		{field{Name: "day", Type: "date", Format: "dd.MM.yyyy"}, "20.11.2026", day, "20.11.2026"},
		{field{Name: "day", Type: "date"}, "2026-11-20", day, "2026-11-20"},
		{field{Name: "j", Type: "json"}, `{"a":[1,2]}`, nil, `{"a":[1,2]}`},
	} {
		c, err := newCodec(tt.f)
		if err != nil {
			t.Fatalf("%+v: %v", tt.f, err)
		}
		v, err := c.parse(tt.text)
		if err != nil {
			t.Errorf("parse %q (%+v): %v", tt.text, tt.f, err)
			continue
		}
		if tt.value != nil || tt.text == "" {
			switch want := tt.value.(type) {
			case decimal.Decimal:
				if d, ok := v.(decimal.Decimal); !ok || !d.Equal(want) {
					t.Errorf("parse %q = %#v, want %v", tt.text, v, want)
				}
			default:
				if v != tt.value {
					t.Errorf("parse %q = %#v, want %#v", tt.text, v, tt.value)
				}
			}
		}
		got, err := c.formatValue(v)
		if err != nil || got != tt.written {
			t.Errorf("format %#v (%+v) = %q, %v; want %q", v, tt.f, got, err, tt.written)
		}
	}
}

func TestCodecErrors(t *testing.T) {
	for _, tt := range []struct {
		f    field
		text string
		want string
	}{
		{field{Name: "i", Type: "int"}, "12a", `"12a" is not a whole number (expected e.g. 1234.5; set a format`},
		{field{Name: "d", Type: "decimal", Format: "#.##0,00"}, "1,2,3", `"1,2,3" is not a number`},
		{field{Name: "b", Type: "bool"}, "maybe", `"maybe" is not true or false`},
		{field{Name: "day", Type: "date", Format: "dd.MM.yyyy"}, "2026-11-20", `does not match the pattern "dd.MM.yyyy"`},
		{field{Name: "t", Type: "datetime"}, "yesterday", `"yesterday" is not a date and time`},
		{field{Name: "j", Type: "json"}, "{", `"{" is not JSON`},
	} {
		c, err := newCodec(tt.f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.parse(tt.text); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%q (%+v): %v, want containing %q", tt.text, tt.f, err, tt.want)
		}
	}
	for _, f := range []field{{Name: "x", Type: "integer"}, {Name: "x", Type: "string", Format: "0"}, {Name: "x", Type: "date", Format: "yyyy-QQ"}} {
		if _, err := newCodec(f); err == nil {
			t.Errorf("%+v accepted", f)
		}
	}
}
