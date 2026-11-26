package actions

import (
	"maps"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/schema"
)

// numberFields returns the paths of the fields of a number type in a schema, also in
// nested objects (e.g. paginate.maxPages), with the schema of their parent.
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
// every built-in action: a number, a number as text (as ${N} gives it once resolved),
// ${N} and {{ … }} are accepted, another text is an error with the name of the field.
func TestNumberFields(t *testing.T) {
	r := Builtin()
	found := map[string]bool{}
	for _, typ := range r.Types() {
		def, _ := r.Lookup(typ)
		d, _ := r.Descriptor(typ)
		base := sampleOf(typ, d.Schema)
		for _, path := range numberFields(d.Schema, "") {
			found[typ+" "+path] = true
			for _, v := range []any{42, "42", " 42 ", "${N}", "{{ params.n }}", "1${N}"} {
				if _, err := def.New(withValue(base, path, v)); err != nil {
					t.Errorf("%s %s: %v: %v", typ, path, v, err)
				}
			}
			want := `field "` + path + `": expected a whole number, got "abc"`
			if _, err := def.New(withValue(base, path, "abc")); err == nil || err.Error() != want {
				t.Errorf("%s %s: abc: %v, want %q", typ, path, err, want)
			}
			want = `field "` + path + `": expected a whole number, got number 1.5`
			if _, err := def.New(withValue(base, path, 1.5)); err == nil || err.Error() != want {
				t.Errorf("%s %s: 1.5: %v, want %q", typ, path, err, want)
			}
		}
	}
	// The fields of the roadmap (R5b2) are among them.
	for _, f := range []string{"archive.extract maxFiles", "http.request paginate.maxPages", "http.request paginate.maxItems",
		"http.request paginate.start", "http.request paginate.size", "http.request retryAfter.maxRetries"} {
		if !found[f] {
			t.Errorf("number field %s not found", f)
		}
	}
}

// TestNumberFieldsAsText checks number fields of type string (they may also hold an
// expression in the editor) by the same rule: checked with the configuration, a
// variable or an expression once it is resolved.
func TestNumberFieldsAsText(t *testing.T) {
	r := Builtin()
	for _, tt := range []struct{ typ, field, number string }{
		{"db.query", "maxRows", "500"},
		{"http.response", "status", "404"},
	} {
		def, _ := r.Lookup(tt.typ)
		d, _ := r.Descriptor(tt.typ)
		base := sampleOf(tt.typ, d.Schema)
		for _, v := range []string{tt.number, "${N}", "{{ params.n }}"} {
			if _, err := def.New(withValue(base, tt.field, v)); err != nil {
				t.Errorf("%s %s: %v: %v", tt.typ, tt.field, v, err)
			}
		}
		want := `field "` + tt.field + `": expected a whole number, got "abc"`
		if _, err := def.New(withValue(base, tt.field, "abc")); err == nil || err.Error() != want {
			t.Errorf("%s %s: abc: %v, want %q", tt.typ, tt.field, err, want)
		}
	}
	// paginate: sizeParam needs size, which may be a variable or an expression.
	req, _ := r.Lookup("http.request")
	for _, size := range []any{"${SIZE}", "{{ params.size }}", 50} {
		if _, err := req.New(map[string]any{"url": "https://example.com/", "paginate": map[string]any{"style": "page", "sizeParam": "per_page", "size": size}}); err != nil {
			t.Errorf("paginate size %v: %v", size, err)
		}
	}
	if _, err := req.New(map[string]any{"url": "https://example.com/", "paginate": map[string]any{"style": "page", "sizeParam": "per_page"}}); err == nil || err.Error() != "paginate: sizeParam needs size" {
		t.Errorf("paginate without size: %v", err)
	}
	// A list of numbers: each entry by the same rule.
	def, _ := r.Lookup("shell.run")
	if _, err := def.New(map[string]any{"program": "robocopy", "okCodes": []any{0, "1", "${OK}", "{{ 2 }}"}}); err != nil {
		t.Errorf("okCodes: %v", err)
	}
	if _, err := def.New(map[string]any{"program": "robocopy", "okCodes": []any{0, "two"}}); err == nil || err.Error() != `field "okCodes[1]": expected a whole number, got "two"` {
		t.Errorf("okCodes: %v", err)
	}
}
