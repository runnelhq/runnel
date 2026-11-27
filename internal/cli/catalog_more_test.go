package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// A name that is an action and a transform gives both descriptors, as a list in JSON.
func TestCatalogActionAndTransform(t *testing.T) {
	code, out, errOut := run(t, "catalog", "--json", "core.script")
	var list []struct{ Type, Kind string }
	if err := json.Unmarshal([]byte(out), &list); code != 0 || err != nil {
		t.Fatalf("%d %v %s\n%s", code, err, errOut, out)
	}
	if len(list) != 2 || list[0].Kind != "action" || list[1].Kind != "transform" || list[0].Type != "core.script" || list[1].Type != "core.script" {
		t.Errorf("descriptors %+v", list)
	}
}

// Nested objects and lists of objects in the configuration are listed with their path
// and the facts on the next line (type list of …).
func TestCatalogNestedFields(t *testing.T) {
	for _, tt := range []struct {
		typ   string
		facts map[string]string
	}{
		{"http.request", map[string]string{"paginate": "object", "paginate.style": "string, required, one of link|url|cursor|page|offset"}},
		{"csv.input", map[string]string{"fields": "list of object", "fields[].name": "string, required"}},
	} {
		code, out, errOut := run(t, "catalog", tt.typ)
		if code != 0 {
			t.Fatalf("%s: %d %s", tt.typ, code, errOut)
		}
		lines := strings.Split(out, "\n")
		for name, want := range tt.facts {
			found := false
			for i, l := range lines[:len(lines)-1] {
				if f := strings.Fields(l); len(f) > 0 && f[0] == name {
					found = strings.HasPrefix(strings.TrimSpace(lines[i+1]), want)
					break
				}
			}
			if !found {
				t.Errorf("%s: field %s with %q not in\n%s", tt.typ, name, want, out)
			}
		}
	}
}
