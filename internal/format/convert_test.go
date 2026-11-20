package format

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
)

func TestConvert(t *testing.T) {
	d := decimal.RequireFromString
	day := record.Date{Year: 2026, Month: time.November, Day: 20}
	at := time.Date(2026, 11, 20, 14, 30, 0, 0, time.UTC)
	for _, tt := range []struct {
		typ, pattern string
		in, want     any
	}{
		{"decimal", "#.##0,00", "1.234,56", d("1234.56")},
		{"int", "", "42", int64(42)},
		{"int", "", "", nil},
		{"int", "", nil, nil},
		{"string", "#.##0,00", d("1234.5"), "1.234,50"},
		{"string", "dd.MM.yyyy", day, "20.11.2026"},
		{"string", "", int64(7), "7"},
		{"string", "", true, "true"},
		{"int", "", d("12"), int64(12)},
		{"int", "", 12.0, int64(12)},
		{"decimal", "", int64(3), d("3")},
		{"decimal", "", 0.1, d("0.1")},
		{"float", "", d("2.5"), 2.5},
		{"float", "", int64(2), 2.0},
		{"date", "", at, day},
		{"date", "dd.MM.yyyy", "20.11.2026", day},
		{"datetime", "", day, day.In(time.Local)},
		{"bool", "", "yes", true},
		{"json", "", `[1,"a"]`, nil}, // checked below
		{"decimal", "", d("1.5"), d("1.5")},
	} {
		c, err := New(record.Type(tt.typ), tt.pattern)
		if tt.typ == "string" {
			c, err = ToText(tt.pattern)
		}
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.Convert(tt.in)
		if err != nil {
			t.Errorf("%v to %s: %v", tt.in, tt.typ, err)
			continue
		}
		switch want := tt.want.(type) {
		case decimal.Decimal:
			if g, ok := got.(decimal.Decimal); !ok || !g.Equal(want) {
				t.Errorf("%v to %s = %#v, want %v", tt.in, tt.typ, got, want)
			}
		case time.Time:
			if g, ok := got.(time.Time); !ok || !g.Equal(want) {
				t.Errorf("%v to %s = %#v, want %v", tt.in, tt.typ, got, want)
			}
		case nil:
			if tt.typ == "json" {
				if l, ok := got.([]any); !ok || len(l) != 2 || l[1] != "a" {
					t.Errorf("json: %#v", got)
				}
			} else if got != nil {
				t.Errorf("%v to %s = %#v, want nil", tt.in, tt.typ, got)
			}
		default:
			if got != tt.want {
				t.Errorf("%v to %s = %#v (%T), want %#v (%T)", tt.in, tt.typ, got, got, tt.want, tt.want)
			}
		}
	}
}

func TestConvertErrors(t *testing.T) {
	for _, tt := range []struct {
		typ  string
		in   any
		want string
	}{
		{"int", decimal.RequireFromString("1.5"), "1.5 is not a whole number"},
		{"int", 2.5, "2.5 is not a whole number"},
		{"int", "1,5", `"1,5" is not a whole number`},
		{"date", int64(3), "a value of type int cannot be converted to date"},
		{"bool", int64(1), "a value of type int cannot be converted to bool"},
		{"decimal", true, "a value of type bool cannot be converted to decimal"},
		{"string", []byte{1}, "binary values cannot be written as text"},
	} {
		c, _ := New(record.Type(tt.typ), "")
		if _, err := c.Convert(tt.in); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v to %s: %v, want %q", tt.in, tt.typ, err, tt.want)
		}
	}
}
