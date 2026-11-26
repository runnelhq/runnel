package action

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/schema"
)

// Nothing is deprecated in the built-in types yet (ADR 0029); these test definitions
// exercise the mechanism.

type transferConfig struct {
	Source   string `json:"source" doc:"File to copy" schema:"required"`
	IfExists string `json:"ifExists" doc:"If the target exists" schema:"enum=replace|fail,default=fail"`
	Legacy   bool   `json:"legacy" doc:"Old behaviour"`
	Fields   []struct {
		Name string `json:"name" doc:"Field" schema:"required"`
		As   string `json:"as" doc:"New name"`
	} `json:"fields" doc:"Fields"`
}

var transferDeprecated = []Deprecated{
	{Field: "overwrite", RenamedTo: "ifExists", Use: "ifExists: replace", RemovedIn: "2.0",
		Schema: &schema.Schema{Type: "boolean", Description: "Replace an existing target"},
		Convert: func(old any) (any, error) {
			switch old {
			case true, "true":
				return "replace", nil
			case false, "false":
				return "fail", nil
			}
			return nil, fmt.Errorf("expected true or false, got %v", old)
		}},
	{Field: "fields[].rename", RenamedTo: "as", RemovedIn: "2.0"},
	{Field: "legacy", RemovedIn: "2.0"},
}

// newTransferTest registers a test type whose New records the configuration it gets.
func newTransferTest(t *testing.T) (*Registry, *transferConfig) {
	t.Helper()
	got := &transferConfig{}
	r := NewRegistry()
	r.Register(Definition{Type: "test.copy", Category: CategoryFiles, Label: "Copy", Description: "Copies a file",
		Config: transferConfig{}, Deprecated: transferDeprecated,
		New: func(with map[string]any) (Action, error) {
			*got = transferConfig{}
			if err := DecodeConfig(with, got); err != nil {
				return nil, err
			}
			return newNothing(with)
		}})
	return r, got
}

func TestDeprecatedMapsOldFields(t *testing.T) {
	r, got := newTransferTest(t)
	def, _ := r.Lookup("test.copy")
	with := map[string]any{"source": "a.csv", "overwrite": true,
		"fields": []any{map[string]any{"name": "a", "rename": "b"}, map[string]any{"name": "c", "as": "d"}}}
	if _, err := def.New(with); err != nil {
		t.Fatal(err)
	}
	if got.IfExists != "replace" || got.Fields[0].As != "b" || got.Fields[1].As != "d" {
		t.Errorf("mapped to %+v", got)
	}
	// The configuration of the file stays as written.
	if _, ok := with["overwrite"]; !ok || with["fields"].([]any)[0].(map[string]any)["rename"] != "b" {
		t.Errorf("configuration changed: %v", with)
	}
	// The new names work as before; a variable is converted only once it is resolved.
	if _, err := def.New(map[string]any{"source": "a.csv", "ifExists": "fail"}); err != nil || got.IfExists != "fail" {
		t.Errorf("new name: %v, %+v", err, got)
	}
	if _, err := def.New(map[string]any{"source": "a.csv", "overwrite": "${OVERWRITE}"}); err != nil || got.IfExists != "${OVERWRITE}" {
		t.Errorf("variable: %v, %+v", err, got)
	}
	// A field that is only deprecated reaches New unchanged.
	if _, err := def.New(map[string]any{"source": "a.csv", "legacy": true}); err != nil || !got.Legacy {
		t.Errorf("legacy: %v, %+v", err, got)
	}
}

func TestDeprecatedRejects(t *testing.T) {
	r, _ := newTransferTest(t)
	def, _ := r.Lookup("test.copy")
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"source": "a", "overwrite": true, "ifExists": "fail"},
			`fields "overwrite" and "ifExists" cannot be used together; overwrite is deprecated, use ifExists`},
		{map[string]any{"source": "a", "fields": []any{map[string]any{"name": "a"}, map[string]any{"name": "b", "rename": "x", "as": "y"}}},
			`fields[1]: fields "rename" and "as" cannot be used together; rename is deprecated, use as`},
		{map[string]any{"source": "a", "overwrite": "maybe"}, `field "overwrite": expected true or false, got maybe`},
		// Unknown fields stay errors.
		{map[string]any{"source": "a", "overwrites": true}, `unknown field "overwrites"`},
	} {
		if _, err := def.New(tt.with); err == nil || err.Error() != tt.want {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
}

func TestFindDeprecated(t *testing.T) {
	with := map[string]any{"source": "a", "overwrite": true, "legacy": false,
		"fields": []any{map[string]any{"name": "a", "rename": "b"}, "x", map[string]any{"name": "c", "rename": "d"}}}
	got := FindDeprecated(with, transferDeprecated)
	want := []DeprecatedUse{
		{".overwrite", `field "overwrite" is deprecated: use ifExists: replace (removed in 2.0)`},
		{".fields[0].rename", `field "rename" is deprecated: use as (removed in 2.0)`},
		{".fields[2].rename", `field "rename" is deprecated: use as (removed in 2.0)`},
		{".legacy", `field "legacy" is deprecated: do not use it any more (removed in 2.0)`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
	if got := FindDeprecated(map[string]any{"source": "a", "ifExists": "fail", "fields": "x"}, transferDeprecated); len(got) != 0 {
		t.Errorf("new names: %v", got)
	}
	nested := []Deprecated{{Field: "auth.user", RenamedTo: "name"}}
	if got := FindDeprecated(map[string]any{"auth": map[string]any{"user": "u"}}, nested); len(got) != 1 || got[0].Path != ".auth.user" {
		t.Errorf("nested: %v", got)
	}
	if out, err := MapDeprecated(map[string]any{"auth": map[string]any{"user": "u"}}, nested); err != nil || out["auth"].(map[string]any)["name"] != "u" {
		t.Errorf("nested: %v, %v", out, err)
	}
}

func TestDeprecatedInSchema(t *testing.T) {
	r, _ := newTransferTest(t)
	d, _ := r.Descriptor("test.copy")
	var names []string
	for _, p := range d.Schema.Properties {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "source,ifExists,overwrite,legacy,fields" {
		t.Errorf("properties %v", names)
	}
	data, err := json.Marshal(d.Schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"overwrite":{"type":"boolean","description":"Replace an existing target","deprecated":true,"x-runnel-replacement":"use ifExists: replace (removed in 2.0)"}`,
		`"legacy":{"type":"boolean","description":"Old behaviour","deprecated":true,"x-runnel-replacement":"do not use it any more (removed in 2.0)"}`,
		`"rename":{"type":"string","description":"New name","deprecated":true,"x-runnel-replacement":"use as (removed in 2.0)"}`,
		`"ifExists":{"type":"string","description":"If the target exists","enum":["replace","fail"],"default":"fail"}`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("schema lacks %s:\n%s", want, data)
		}
	}
}

func TestDeprecatedRegisterPanics(t *testing.T) {
	for _, tt := range []struct {
		dep  Deprecated
		want string
	}{
		{Deprecated{Field: "source", RenamedTo: "ifExists"}, "renamed field source: still in the configuration"},
		{Deprecated{Field: "old", RenamedTo: "missing"}, `renamed field old: no field "missing"`},
		{Deprecated{Field: "gone"}, "deprecated field gone: not in the configuration"},
		{Deprecated{Field: "nope[].x", RenamedTo: "y"}, `deprecated field nope[].x: no field "nope"`},
		{Deprecated{Field: "source[].x", RenamedTo: "y"}, `deprecated field source[].x: "source" is not a list`},
	} {
		func() {
			defer func() {
				msg := fmt.Sprint(recover())
				if !strings.Contains(msg, tt.want) {
					t.Errorf("%s: panic %q, want %q", tt.dep.Field, msg, tt.want)
				}
			}()
			NewRegistry().Register(Definition{Type: "test.copy", Config: transferConfig{}, Deprecated: []Deprecated{tt.dep}, New: newNothing})
		}()
	}
}
