package action

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type copyConfig struct {
	Source string `json:"source" doc:"File to copy" schema:"required"`
	Target struct {
		Path string `json:"path" doc:"Target folder" schema:"required"`
	} `json:"target" doc:"Where to copy to"`
}

func newNothing(map[string]any) (Action, error) {
	return Func(func(context.Context, *ExecContext) (Result, error) { return Result{}, nil }), nil
}

func TestRegisterDescriptors(t *testing.T) {
	r := NewRegistry()
	r.Register(Definition{Type: "test.copy", Category: CategoryFiles, Label: "Copy", Description: "Copies a file", Config: copyConfig{}, New: newNothing})
	r.Register(Definition{Type: "test.empty", Category: CategoryGeneral, Label: "Empty", Description: "Does nothing", New: newNothing})

	descs := r.Descriptors()
	if len(descs) != 2 || descs[0].Type != "test.copy" || descs[1].Type != "test.empty" {
		t.Fatalf("descriptors %+v", descs)
	}
	got, err := json.Marshal(descs[1])
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"test.empty","kind":"action","category":"general","label":"Empty","description":"Does nothing",` +
		`"schema":{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false}}`
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	d, ok := r.Descriptor("test.copy")
	if !ok || d.Schema.Required[0] != "source" || len(d.Schema.Properties) != 2 {
		t.Errorf("descriptor %+v", d)
	}
	if _, ok := r.Descriptor("test.none"); ok {
		t.Error("unknown type found")
	}
}

func TestRegisterPanics(t *testing.T) {
	tests := []struct {
		def  Definition
		want string
	}{
		{Definition{Type: "copy", New: newNothing}, `invalid action type name "copy"`},
		{Definition{Type: "test.bad", Config: struct {
			C chan int `json:"c"`
		}{}, New: newNothing}, `action type test.bad: field "c": unsupported type`},
		{Definition{Type: "test.dup", New: newNothing}, "action type registered twice: test.dup"},
	}
	r := NewRegistry()
	r.Register(Definition{Type: "test.dup", New: newNothing})
	for _, tt := range tests {
		func() {
			defer func() {
				if p := recover(); p == nil || !strings.Contains(p.(string), tt.want) {
					t.Errorf("%s: panic %v, want containing %q", tt.def.Type, p, tt.want)
				}
			}()
			r.Register(tt.def)
		}()
	}
}

func TestDecodeConfig(t *testing.T) {
	tests := []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"source": "a.csv"}, ""},
		{map[string]any{"source": "a.csv", "target": map[string]any{"path": "out"}}, ""},
		{map[string]any{}, `missing required field "source"`},
		{map[string]any{"source": "a.csv", "target": map[string]any{}}, `missing required field "target.path"`},
		{map[string]any{"source": "a.csv", "colour": "red"}, `unknown field "colour"`},
		{map[string]any{"source": 1}, `field "source": expected string, got number`},
	}
	for _, tt := range tests {
		var c copyConfig
		err := DecodeConfig(tt.with, &c)
		if (tt.want == "" && err != nil) || (tt.want != "" && (err == nil || err.Error() != tt.want)) {
			t.Errorf("%v: error %v, want %q", tt.with, err, tt.want)
		}
	}
}
