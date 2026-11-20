package record

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestNormalizeAndTypeOf(t *testing.T) {
	day := Date{2026, time.November, 20}
	now := time.Date(2026, 11, 20, 14, 30, 0, 0, time.UTC)
	for _, tt := range []struct {
		in   any
		want any
		typ  Type
	}{
		{"x", "x", String},
		{7, int64(7), Int},
		{uint64(7), int64(7), Int},
		{int32(-3), int64(-3), Int},
		{float32(1.5), 1.5, Float},
		{decimal.RequireFromString("19.90"), decimal.RequireFromString("19.90"), Decimal},
		{true, true, Bool},
		{day, day, DateType},
		{now, now, DateTime},
		{[]byte("ab"), []byte("ab"), Binary},
		{json.Number("12"), int64(12), Int},
		{json.Number("1.25"), 1.25, Float},
		{nil, nil, Unknown},
	} {
		got, err := Normalize(tt.in)
		if err != nil {
			t.Errorf("Normalize(%#v): %v", tt.in, err)
			continue
		}
		if TypeOf(got) != tt.typ {
			t.Errorf("TypeOf(Normalize(%#v)) = %q, want %q", tt.in, TypeOf(got), tt.typ)
		}
		if d, ok := got.(decimal.Decimal); ok {
			if !d.Equal(tt.want.(decimal.Decimal)) {
				t.Errorf("%v != %v", d, tt.want)
			}
			continue
		}
		if b, ok := got.([]byte); ok {
			if string(b) != string(tt.want.([]byte)) {
				t.Errorf("%v != %v", b, tt.want)
			}
			continue
		}
		if got != tt.want {
			t.Errorf("Normalize(%#v) = %#v, want %#v", tt.in, got, tt.want)
		}
	}

	// Nested JSON is normalized throughout.
	got, err := Normalize(map[string]any{"a": []any{uint64(1), map[string]any{"b": 2}}})
	if err != nil || TypeOf(got) != JSON || got.(map[string]any)["a"].([]any)[0] != int64(1) {
		t.Errorf("nested: %#v %v", got, err)
	}
	for _, bad := range []any{struct{}{}, uint64(math.MaxUint64), map[string]any{"x": struct{}{}}} {
		if _, err := Normalize(bad); err == nil {
			t.Errorf("Normalize(%#v) accepted", bad)
		}
	}
}

func TestTypes(t *testing.T) {
	for _, typ := range Types {
		if !typ.Valid() {
			t.Errorf("%q not valid", typ)
		}
	}
	if Unknown.Valid() || Type("integer").Valid() {
		t.Error("invalid type accepted")
	}
}

func TestDate(t *testing.T) {
	d, err := ParseDate("2026-02-28")
	if err != nil || d.String() != "2026-02-28" {
		t.Fatalf("%v %v", d, err)
	}
	if _, err := ParseDate("28.02.2026"); err == nil || !strings.Contains(err.Error(), "expected YYYY-MM-DD") {
		t.Errorf("err = %v", err)
	}
	// The day of a time in its own zone, not UTC.
	berlin := time.FixedZone("CET", 3600)
	if got := DateOf(time.Date(2026, 3, 1, 0, 30, 0, 0, berlin)); got.String() != "2026-03-01" {
		t.Errorf("DateOf = %s", got)
	}
	if !d.Before(Date{2026, 3, 1}) || (Date{2026, 3, 1}).Before(d) {
		t.Error("Before")
	}
	b, _ := json.Marshal(map[string]any{"d": d})
	if string(b) != `{"d":"2026-02-28"}` {
		t.Errorf("json %s", b)
	}
	var back Date
	if err := back.UnmarshalText([]byte("2026-02-28")); err != nil || back != d {
		t.Errorf("unmarshal %v %v", back, err)
	}
}

func TestSchema(t *testing.T) {
	s := Schema{}.With("id", Int).With("name", String).With("id", Decimal)
	if f, ok := s.Field("id"); !ok || f.Type != Decimal || len(s.Fields) != 2 {
		t.Errorf("With: %+v", s)
	}
	a := Schema{Fields: []FieldDef{{"id", Int}, {"name", String}}}
	b := Schema{Fields: []FieldDef{{"id", Int}, {"city", String}}}
	m := Merge(a, b)
	want := []FieldDef{{"id", Int}, {"name", Unknown}, {"city", Unknown}}
	if m.Open || len(m.Fields) != 3 || m.Fields[0] != want[0] || m.Fields[1] != want[1] || m.Fields[2] != want[2] {
		t.Errorf("Merge = %+v", m)
	}
	if !Merge(a, OpenSchema()).Open || Merge(a).Open || Merge().Open || len(Merge().Fields) != 0 {
		t.Error("Merge open")
	}
	if got := Merge(a, Schema{Fields: []FieldDef{{"id", String}}}); got.Fields[0].Type != Unknown {
		t.Errorf("different types: %+v", got)
	}
}
