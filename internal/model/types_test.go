package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
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
		{TypeInteger, "42", int64(42), ""},
		{TypeInteger, " -7 ", int64(-7), ""},
		{TypeInteger, "4.2", nil, "not an integer"},
		{TypeNumber, "2.5", 2.5, ""},
		{TypeNumber, "1e3", 1000.0, ""},
		{TypeNumber, "x", nil, "not a number"},
		{TypeBoolean, "TRUE", true, ""},
		{TypeBoolean, "false", false, ""},
		{TypeBoolean, "yes", nil, "not a boolean"},
		{TypeJSON, `{"a": [1, "b"]}`, map[string]any{"a": []any{json.Number("1"), "b"}}, ""},
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
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s.Parse(%q) = (%#v, %v), want %#v", tt.typ, tt.in, got, err, tt.want)
		}
	}
}

func TestParameterCheckValue(t *testing.T) {
	p := Parameter{Name: "N", Type: TypeInteger, Enum: []string{"1", "3", "5"}}
	if v, err := p.CheckValue("03"); err != nil || v != int64(3) {
		t.Errorf("03: (%v, %v), want 3 (enum compares typed values)", v, err)
	}
	if _, err := p.CheckValue("2"); err == nil || !strings.Contains(err.Error(), "allowed values 1, 3, 5") {
		t.Errorf("2: %v", err)
	}
	if _, err := p.CheckValue("x"); err == nil || !strings.Contains(err.Error(), "not an integer") {
		t.Errorf("x: %v", err)
	}
	free := Parameter{Name: "S"}
	if v, err := free.CheckValue("anything"); err != nil || v != "anything" {
		t.Errorf("untyped: (%v, %v)", v, err)
	}
}
