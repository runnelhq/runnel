package record

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestCompare(t *testing.T) {
	d := decimal.RequireFromString
	day := func(n int) Date { return Date{2026, time.November, n} }
	for _, tt := range []struct {
		a, b any
		want int
	}{
		{int64(1), int64(2), -1},
		{int64(2), d("1.5"), 1},
		{d("0.1"), 0.1, 0},
		{2.5, int64(2), 1},
		{"Apfel", "apfel", -1}, // code points: upper case first
		{"a", "a", 0},
		{false, true, -1},
		{day(20), day(19), 1},
		{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 1, 0, 0, 0, time.FixedZone("CET", 3600)), 0},
	} {
		if got := Compare(tt.a, tt.b); got != tt.want {
			t.Errorf("Compare(%v, %v) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
	for v, want := range map[any]Kind{nil: KindNone, int64(1): KindNumber, 1.5: KindNumber, "x": KindString, true: KindBool} {
		if got := KindOf(v); got != want {
			t.Errorf("KindOf(%v) = %q, want %q", v, got, want)
		}
	}
	if KindOf(map[string]any{}) != Incomparable || KindOf(d("1")) != KindNumber || KindOf(day(1)) != KindDate {
		t.Error("KindOf")
	}
}

func TestKey(t *testing.T) {
	d := decimal.RequireFromString
	same := [][2]any{{int64(1), d("1.0")}, {d("1.50"), 1.5}, {nil, nil}, {"a", "a"}}
	for _, p := range same {
		if Key(p[0]) != Key(p[1]) {
			t.Errorf("Key(%v) != Key(%v)", p[0], p[1])
		}
	}
	different := [][2]any{{int64(1), "1"}, {nil, ""}, {"true", true}, {Date{2026, 1, 2}, "2026-01-02"}}
	for _, p := range different {
		if Key(p[0]) == Key(p[1]) {
			t.Errorf("Key(%v) == Key(%v)", p[0], p[1])
		}
	}
}
