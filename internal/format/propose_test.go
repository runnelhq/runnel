package format

import (
	"testing"

	"github.com/runnelhq/runnel/internal/record"
)

func TestPropose(t *testing.T) {
	tests := []struct {
		name    string
		values  []string
		typ     record.Type
		format  string
		note    string
		warning bool
	}{
		{"int", []string{"4711", "4712", ""}, record.Int, "", "", false},
		{"decimal", []string{"1.5", "-2", "3.25"}, record.Decimal, "", "", false},
		{"german", []string{"1.234,50", "89,90", "12.500,00"}, record.Decimal, "#.##0,00", "", false},
		{"english", []string{"1,234.50", "89.90", "12,500.00"}, record.Decimal, "#,##0.00", "", false},
		{"iso date", []string{"2026-11-19", "2026-11-20"}, record.DateType, "", "", false},
		{"german date", []string{"19.11.2026", "01.12.2026"}, record.DateType, "dd.MM.yyyy", "", false},
		{"german datetime", []string{"19.11.2026 14:30"}, record.DateTime, "dd.MM.yyyy HH:mm", "", false},
		{"bool", []string{"true", "false", "yes"}, record.Bool, "", "", false},
		{"text", []string{"Köln", "Bonn"}, record.String, "", "", false},
		{"leading zero", []string{"0221 123456", "", "0241 98765"}, record.String, "", "Leading zero: kept as text", false},
		{"leading zero number", []string{"0221", "0241"}, record.String, "", "Leading zero: kept as text", false},
		{"zero", []string{"0", "0,00", "980,50"}, record.Decimal, "#.##0,00", "", false},
		{"some do not fit", []string{"12.500,00", "0,00", "980,50", "k. A."}, record.String, "",
			"3 of 4 values are decimal (#.##0,00); “k. A.” in line 5 is not. Kept as text.", true},
		{"ja/nein", []string{"ja", "nein", "Ja"}, record.String, "", "ja/nein is not a bool (true/false, 1/0, yes/no): kept as text", false},
		{"empty", []string{"", " "}, record.String, "", "No values: kept as text", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := make([]int, len(tt.values))
			for i := range lines {
				lines[i] = i + 2
			}
			p := Propose(tt.values, lines)
			if p.Type != tt.typ || p.Format != tt.format || (tt.note != "" && p.Note != tt.note) || p.Warning != tt.warning {
				t.Errorf("Propose(%q) = %+v; want %s %q note %q warning %v", tt.values, p, tt.typ, tt.format, tt.note, tt.warning)
			}
		})
	}
}
