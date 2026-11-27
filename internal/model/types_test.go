package model

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
)

func TestValueTypeParse(t *testing.T) {
	tests := []struct {
		typ     ValueType
		in      string
		want    any
		wantErr string
	}{
		{"", "text", "text", ""},
		{TypeString, " 1 ", " 1 ", ""},
		{TypeInt, "42", int64(42), ""},
		{TypeInt, " -7 ", int64(-7), ""},
		{TypeInt, "4.2", nil, "not a whole number"},
		{TypeInt, "", nil, "empty text is not a value of type int"},
		{TypeDecimal, "12.50", decimal.RequireFromString("12.50"), ""},
		{TypeDecimal, "1.234,5", nil, "not a number"},
		{TypeFloat, "2.5", 2.5, ""},
		{TypeFloat, "1e3", 1000.0, ""},
		{TypeFloat, "x", nil, "not a number"},
		{TypeBool, "TRUE", true, ""},
		{TypeBool, "false", false, ""},
		{TypeBool, "yes", true, ""},
		{TypeBool, "0", false, ""},
		{TypeBool, "maybe", nil, "not true or false"},
		{TypeDate, "2026-11-20", record.Date{Year: 2026, Month: time.November, Day: 20}, ""},
		{TypeDate, "20.11.2026", nil, "not a date (expected YYYY-MM-DD)"},
		{TypeDateTime, "2026-11-20T14:30:00Z", time.Date(2026, 11, 20, 14, 30, 0, 0, time.UTC), ""},
		{TypeDateTime, "2026-11-20 14:30:00", time.Date(2026, 11, 20, 14, 30, 0, 0, time.Local), ""},
		{TypeDateTime, "noon", nil, "not a date and time (expected e.g. 2026-11-20T14:30:00Z"},
		{TypeJSON, `{"a": [1, 1.5, "b"]}`, map[string]any{"a": []any{int64(1), 1.5, "b"}}, ""},
		{TypeJSON, `"x"`, "x", ""},
		{TypeJSON, `{"a":`, nil, "invalid JSON"},
		{TypeJSON, `1 2`, nil, "unexpected data"},
	}
	for _, tt := range tests {
		got, err := tt.typ.Parse(tt.in)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%s.Parse(%q) error = %v, want containing %q", tt.typ, tt.in, err, tt.wantErr)
			}
			continue
		}
		if err != nil || record.Key(got) != record.Key(tt.want) || reflect.TypeOf(got) != reflect.TypeOf(tt.want) {
			t.Errorf("%s.Parse(%q) = (%#v, %v), want %#v", tt.typ, tt.in, got, err, tt.want)
		}
	}
}

func TestParameterCheckValue(t *testing.T) {
	p := Parameter{Name: "N", Type: TypeInt, Enum: []string{"1", "3", "5"}}
	if v, err := p.CheckValue("03"); err != nil || v != int64(3) {
		t.Errorf("03: (%v, %v), want 3 (enum compares typed values)", v, err)
	}
	if _, err := p.CheckValue("2"); err == nil || !strings.Contains(err.Error(), "allowed values 1, 3, 5") {
		t.Errorf("2: %v", err)
	}
	if _, err := p.CheckValue("x"); err == nil || !strings.Contains(err.Error(), "not a whole number") {
		t.Errorf("x: %v", err)
	}
	amount := Parameter{Name: "A", Type: TypeDecimal, Enum: []string{"1", "2.5"}}
	if _, err := amount.CheckValue("2.50"); err != nil {
		t.Errorf("2.50: %v, want allowed (numbers compare exactly)", err)
	}
	if _, err := (Parameter{Name: "T", Type: "integer"}).CheckValue("1"); err == nil || !strings.Contains(err.Error(), `unknown type "integer"`) {
		t.Errorf("old type name: %v", err)
	}
	free := Parameter{Name: "S"}
	if v, err := free.CheckValue("anything"); err != nil || v != "anything" {
		t.Errorf("untyped: (%v, %v)", v, err)
	}
}
