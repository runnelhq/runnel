package transforms

import (
	"maps"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/schema"
)

// numberFields returns the paths of the fields of a number type in a schema, also in
// nested objects.
func numberFields(s *schema.Schema, prefix string) []string {
	var out []string
	for _, p := range s.Properties {
		switch p.Schema.Type {
		case "integer", "number":
			out = append(out, prefix+p.Name)
		case "object":
			out = append(out, numberFields(p.Schema, prefix+p.Name+".")...)
		}
	}
	return out
}

// withValue returns a copy of with in which the field at path (a.b) has the value v.
func withValue(with map[string]any, path string, v any) map[string]any {
	out := maps.Clone(with)
	name, rest, nested := strings.Cut(path, ".")
	if !nested {
		out[name] = v
		return out
	}
	inner, _ := out[name].(map[string]any)
	if inner == nil {
		inner = map[string]any{}
	}
	out[name] = withValue(inner, rest, v)
	return out
}

// TestNumberFields checks the rule of the number fields (R5b2) for every number field of
// every built-in transform: a number, a number as text (as ${N} gives it once resolved)
// and ${N} are accepted; another text and {{ … }}, which the configuration of a transform
// does not evaluate, are errors with the name of the field.
func TestNumberFields(t *testing.T) {
	r := Builtin()
	found := map[string]bool{}
	for _, typ := range r.Types() {
		def, _ := r.Lookup(typ)
		d, _ := r.Descriptor(typ)
		base := sampleOf(typ, d.Schema)
		for _, path := range numberFields(d.Schema, "") {
			found[typ+" "+path] = true
			for _, v := range []any{42, "42", "${N}"} {
				if _, err := def.New(withValue(base, path, v)); err != nil {
					t.Errorf("%s %s: %v: %v", typ, path, v, err)
				}
			}
			for _, v := range []string{"abc", "{{ 42 }}"} {
				want := `field "` + path + `": expected a whole number, got "` + v + `"`
				if _, err := def.New(withValue(base, path, v)); err == nil || err.Error() != want {
					t.Errorf("%s %s: %s: %v, want %q", typ, path, v, err, want)
				}
			}
		}
	}
	for _, f := range []string{"db.table-output commitSize", "db.upsert commitSize", "db.lookup cache", "core.log every"} {
		if !found[f] {
			t.Errorf("number field %s not found", f)
		}
	}
}

// TestNumberFieldsAsText checks the number fields of type string by the same rule.
func TestNumberFieldsAsText(t *testing.T) {
	r := Builtin()
	for _, tt := range []struct{ typ, field, number string }{
		{"core.generate-rows", "count", "5"},
		{"core.sequence", "start", "10"},
		{"core.sequence", "step", "-2"},
		{"core.to-result", "maxRecords", "10"},
		{"core.stream-lookup", "maxRecords", "10"},
		{"http.response", "maxRecords", "10"},
		{"http.response", "status", "404"},
	} {
		def, _ := r.Lookup(tt.typ)
		d, _ := r.Descriptor(tt.typ)
		base := sampleOf(tt.typ, d.Schema)
		for _, v := range []string{tt.number, "${N}"} {
			if _, err := def.New(withValue(base, tt.field, v)); err != nil {
				t.Errorf("%s %s: %v: %v", tt.typ, tt.field, v, err)
			}
		}
		for _, v := range []string{"abc", "{{ 5 }}"} {
			want := `field "` + tt.field + `": expected a whole number, got "` + v + `"`
			if _, err := def.New(withValue(base, tt.field, v)); err == nil || err.Error() != want {
				t.Errorf("%s %s: %s: %v, want %q", tt.typ, tt.field, v, err, want)
			}
		}
	}
}
