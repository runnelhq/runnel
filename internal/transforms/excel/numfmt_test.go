package excel

import (
	"path/filepath"
	"strings"
	"testing"
)

// Runnel patterns become Excel codes; what Excel cannot show is an error with a hint
// to excelFormat (ADR 0025).
func TestExcelCode(t *testing.T) {
	for _, tt := range []struct{ pattern, want string }{
		{"#.##0,00", "#,##0.00"},
		{"#,##0.00", "#,##0.00"},
		{"#'##0.00", "#,##0.00"},
		{"# ##0", "#,##0"},
		{"#.##0", "#,##0"},
		{"0,000", "0.000"},
		{"0", "0"},
		{"dd.MM.yyyy", "dd.mm.yyyy"},
		{"yyyy-MM-dd HH:mm:ss", "yyyy-mm-dd hh:mm:ss"},
		{"d. MMMM yyyy", "d. mmmm yyyy"},
		{"EEEE, dd.MM.yy", "dddd, dd.mm.yy"},
		{"MM/dd/yyyy hh:mm a", "mm/dd/yyyy hh:mm AM/PM"},
		{"HH:mm:ss.SSS", "hh:mm:ss.000"},
		{"mm:ss", "mm:ss"},
		{"dd.MM.yyyy 'um' HH:mm 'Uhr'", `dd.mm.yyyy \u\m hh:mm \U\h\r`},
	} {
		got, err := excelCode(tt.pattern)
		if err != nil || got != tt.want {
			t.Errorf("%q: %q %v, want %q", tt.pattern, got, err, tt.want)
		}
	}
	for _, tt := range []struct{ pattern, want string }{
		{"0.0%", "digits must be in 'quotes'; for a number format of Excel like 0.0% use excelFormat"},
		{"dd.mm.yyyy", "Excel shows minutes only after hours or before seconds; set excelFormat"},
		{"HH MM", "Excel shows a month after hours or before seconds as minutes"},
		{"dd.MM.yyyy hh:mm", "Excel shows hh without a as 24-hour time; use HH, or add a"},
		{"HH:mm a", "Excel shows HH with a as 12-hour time; use hh with a"},
		{"yyyy-MM-dd'T'HH:mm:ssXXX", "Excel has no time zones"},
		{"HH:mm SSS", "Excel shows milliseconds only as ss.SSS"},
	} {
		if _, err := excelCode(tt.pattern); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%q: %v, want %q", tt.pattern, err, tt.want)
		}
	}
}

// format and excelFormat: one of both, checked when the transform is created.
func TestOutputFormatErrors(t *testing.T) {
	for _, tt := range []struct {
		field map[string]any
		want  string
	}{
		{map[string]any{"name": "a", "format": "0.0%"}, `fields: field "a": date pattern "0.0%": digits must be in 'quotes'`},
		{map[string]any{"name": "a", "format": "#,##0", "excelFormat": "0.0%"}, `fields: field "a": set format or excelFormat, not both`},
	} {
		_, err := newOutput(map[string]any{"path": filepath.Join(t.TempDir(), "a.xlsx"), "fields": []any{tt.field}})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.field, err, tt.want)
		}
	}
}
