package action

import (
	"reflect"
	"testing"
)

type numbersInner struct {
	Max  int  `json:"max"`
	Rate *int `json:"rate"`
}

type numbersConfig struct {
	Count   int64          `json:"count" schema:"required"`
	Share   float64        `json:"share"`
	Size    uint           `json:"size"`
	Inner   numbersInner   `json:"inner"`
	Ptr     *numbersInner  `json:"ptr"`
	List    []numbersInner `json:"list"`
	Codes   []int          `json:"codes"`
	Text    string         `json:"text"`
	Mapping map[string]any `json:"mapping"`
}

// TestDecodeNumbers checks the rule of the number fields (R5b2): numbers as text become
// numbers, also nested and in lists; ${…} (and {{ … }} in actions) leave the default
// until the run starts; other texts are errors with the path of the field.
func TestDecodeNumbers(t *testing.T) {
	with := map[string]any{
		"count": " 42 ", "share": "0.5", "size": "7",
		"inner":   map[string]any{"max": "3", "rate": "${RATE}"},
		"ptr":     map[string]any{"max": 5, "rate": "{{ 2 }}"},
		"list":    []any{map[string]any{"max": "1"}, map[string]any{"max": "${M}"}},
		"codes":   []any{0, "1", "${C}"},
		"text":    "12",
		"mapping": map[string]any{"max": "x"},
	}
	var c numbersConfig
	if err := DecodeConfig(with, &c); err != nil {
		t.Fatal(err)
	}
	want := numbersConfig{Count: 42, Share: 0.5, Size: 7, Inner: numbersInner{Max: 3}, Ptr: &numbersInner{Max: 5},
		List: []numbersInner{{Max: 1}, {}}, Codes: []int{0, 1, 0}, Text: "12", Mapping: map[string]any{"max": "x"}}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("got %+v\nwant %+v", c, want)
	}
	// The configuration itself is not changed.
	if with["count"] != " 42 " || with["inner"].(map[string]any)["max"] != "3" || with["codes"].([]any)[1] != "1" {
		t.Errorf("with changed: %v", with)
	}
	// A required field given as ${…} counts as given.
	if err := DecodeConfig(map[string]any{"count": "${N}"}, &numbersConfig{}); err != nil {
		t.Errorf("required variable: %v", err)
	}

	for _, tt := range []struct {
		with      map[string]any
		transform bool
		want      string
	}{
		{map[string]any{"count": "abc"}, false, `field "count": expected a whole number, got "abc"`},
		{map[string]any{"count": "1.5"}, false, `field "count": expected a whole number, got "1.5"`},
		{map[string]any{"count": 1.5}, false, `field "count": expected a whole number, got number 1.5`},
		{map[string]any{"count": true}, false, `field "count": expected a whole number, got bool`},
		{map[string]any{"count": ""}, false, `field "count": expected a whole number, got ""`},
		{map[string]any{"share": "half"}, false, `field "share": expected a number, got "half"`},
		{map[string]any{"share": "NaN"}, false, `field "share": expected a number, got "NaN"`},
		{map[string]any{"size": "-1"}, false, `field "size": expected a whole number of 0 or more, got "-1"`},
		{map[string]any{"inner": map[string]any{"max": "many"}}, false, `field "inner.max": expected a whole number, got "many"`},
		{map[string]any{"list": []any{map[string]any{"max": "x"}}}, false, `field "list[0].max": expected a whole number, got "x"`},
		{map[string]any{"codes": []any{"one"}}, false, `field "codes[0]": expected a whole number, got "one"`},
		{map[string]any{"count": "99999999999999999999"}, false, `field "count": expected a whole number, got "99999999999999999999"`},
		// The configuration of a transform does not evaluate {{ … }}.
		{map[string]any{"count": "{{ 5 }}"}, true, `field "count": expected a whole number, got "{{ 5 }}"`},
	} {
		decode := DecodeConfig
		if tt.transform {
			decode = DecodeConfigNumbers
		}
		if err := decode(tt.with, &numbersConfig{}); err == nil || err.Error() != tt.want {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
	var c2 numbersConfig
	if err := DecodeConfigNumbers(map[string]any{"count": "${N}", "share": "2"}, &c2); err != nil || c2.Count != 0 || c2.Share != 2 {
		t.Errorf("transform: %+v %v", c2, err)
	}
}

func TestParseWholeNumber(t *testing.T) {
	for _, tt := range []struct {
		text        string
		expressions bool
		n           int64
		ok          bool
		err         string
	}{
		{"42", false, 42, true, ""},
		{" -3 ", false, -3, true, ""},
		{"", false, 0, false, ""},
		{"${N}", false, 0, false, ""},
		{"{{ n }}", true, 0, false, ""},
		{"{{ n }}", false, 0, false, `field "f": expected a whole number, got "{{ n }}"`},
		{"x", true, 0, false, `field "f": expected a whole number, got "x"`},
	} {
		n, ok, err := ParseWholeNumber("f", tt.text, tt.expressions)
		if n != tt.n || ok != tt.ok || (err == nil) != (tt.err == "") || (err != nil && err.Error() != tt.err) {
			t.Errorf("%q: %d %v %v", tt.text, n, ok, err)
		}
	}
}
