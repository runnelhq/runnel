package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// contractDir holds the golden files of the last release tag (ADR 0029, section 7). They
// are updated only by the release process (docs/dev/RELEASE.md), never with -update.
const contractDir = "testdata/contract"

// The JSON outputs promised by ADR 0029 (catalog, expressions, schemas of the file kinds)
// may only grow: every path of the baseline must still exist with the same type, no enum
// value may disappear, no default or type may change and no field may become required.
func TestContractGoldens(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(contractDir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no baseline in %s: %v", contractDir, err)
	}
	for _, base := range files {
		name := filepath.Base(base)
		t.Run(name, func(t *testing.T) {
			old, cur := readJSON(t, base), readJSON(t, filepath.Join("testdata", name))
			breaks := contractBreaks(old, cur)
			for _, p := range breaks {
				t.Errorf("%s: %s", name, p)
			}
			if len(breaks) > 0 {
				t.Log("these keys are promised by ADR 0029 since the last release: keep them and deprecate instead " +
					"(docs/dev/ACTIONS.md); the baseline in " + contractDir + " changes only with a release (docs/dev/RELEASE.md)")
			}
		})
	}
}

func readJSON(t *testing.T, path string) any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return v
}

// Keys whose values are help texts and examples: they may change or go. Their absence or
// type is not a break.
var contractFree = map[string]bool{"example": true, "examples": true, "limits": true, "x-runnel-only": true, "x-runnel-group": true}

// contractBreaks lists what cur breaks of old, each with its JSON path.
func contractBreaks(old, cur any) []string {
	var out []string
	compareContract("$", "", old, cur, &out)
	return out
}

func compareContract(path, key string, old, cur any, out *[]string) {
	add := func(format string, args ...any) { *out = append(*out, path+": "+fmt.Sprintf(format, args...)) }
	if kindOf(old) != kindOf(cur) {
		add("type changed from %s to %s", kindOf(old), kindOf(cur))
		return
	}
	switch o := old.(type) {
	case map[string]any:
		c := cur.(map[string]any)
		for _, k := range sortedKeys(o) {
			if contractFree[k] {
				continue
			}
			cv, ok := c[k]
			if !ok {
				*out = append(*out, fmt.Sprintf("%s.%s: removed", path, k))
				continue
			}
			compareContract(path+"."+k, k, o[k], cv, out)
		}
	case []any:
		compareList(path, key, o, cur.([]any), out)
	default:
		// Defaults and types (of schemas, outputs, functions) keep their value.
		if (key == "default" || key == "type") && !reflect.DeepEqual(old, cur) {
			add("changed from %v to %v", old, cur)
		}
	}
}

// compareList compares lists: those of objects by their name or type, enum values and
// accepted connection types as sets that may only grow, required fields as a set that
// may only shrink. Other lists of values (texts) are free.
func compareList(path, key string, old, cur []any, out *[]string) {
	if objectList(old) {
		curBy := byIdentity(cur)
		for id, o := range byIdentity(old) {
			c, ok := curBy[id]
			if !ok {
				*out = append(*out, fmt.Sprintf("%s[%s]: removed", path, id))
				continue
			}
			compareContract(fmt.Sprintf("%s[%s]", path, id), "", o, c, out)
		}
		return
	}
	missing := func(from, in []any) []string {
		var m []string
		for _, v := range from {
			if !slices.ContainsFunc(in, func(x any) bool { return reflect.DeepEqual(x, v) }) {
				m = append(m, fmt.Sprint(v))
			}
		}
		return m
	}
	switch key {
	case "enum", "x-runnel-connection":
		if m := missing(old, cur); len(m) > 0 {
			*out = append(*out, fmt.Sprintf("%s: values removed: %s", path, strings.Join(m, ", ")))
		}
	case "required":
		if m := missing(cur, old); len(m) > 0 {
			*out = append(*out, fmt.Sprintf("%s: fields now required: %s", path, strings.Join(m, ", ")))
		}
	}
}

func objectList(l []any) bool {
	for _, v := range l {
		if _, ok := v.(map[string]any); !ok {
			return false
		}
	}
	return len(l) > 0
}

// byIdentity indexes objects by "name", else "type", else their position; repeated
// identities get "#2", "#3" ….
func byIdentity(l []any) map[string]any {
	out := map[string]any{}
	for i, v := range l {
		m := v.(map[string]any)
		id, ok := m["name"].(string)
		if !ok {
			id, ok = m["type"].(string)
		}
		if !ok {
			id = fmt.Sprint(i)
		}
		key := id
		for n := 2; out[key] != nil; n++ {
			key = fmt.Sprintf("%s#%d", id, n)
		}
		out[key] = v
	}
	return out
}

func kindOf(v any) string {
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	}
	return fmt.Sprintf("%T", v)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// The check itself: what may change and what is a break.
func TestContractBreaks(t *testing.T) {
	base := `{"version": "v1.0.0", "actions": [
	  {"type": "file.copy", "label": "Copy", "schema": {"type": "object",
	    "properties": {
	      "source": {"type": "string", "examples": ["a.csv"]},
	      "ifExists": {"type": "string", "enum": ["replace", "skip", "fail"], "default": "fail"},
	      "retry": {"type": "integer"}},
	    "required": ["source"]},
	   "outputs": [{"name": "output.files", "type": "list"}], "limits": ["at most 10"]},
	  {"type": "file.move", "label": "Move"}],
	  "functions": [{"name": "upper", "type": "string"}, {"name": "upper", "type": "string"}]}`
	for _, tt := range []struct {
		name, from, to string
		want           []string
	}{
		{"unchanged", "", "", nil},
		{"new keys, types, enum values and order", `"actions": [`,
			`"new": {"a": 1}, "actions": [{"type": "file.zip", "label": "Zip"}, `, nil},
		{"new enum value", `"skip", "fail"]`, `"skip", "fail", "rename"]`, nil},
		{"texts and examples change", `"label": "Copy"`, `"label": "Copy files"`, nil},
		{"version changes", `"v1.0.0"`, `"v1.1.0"`, nil},
		{"example removed", `, "examples": ["a.csv"]`, ``, nil},
		{"limits removed", `, "limits": ["at most 10"]`, ``, nil},
		{"field required no more", `"required": ["source"]`, `"required": []`, nil},
		{"field removed", `"retry": {"type": "integer"}`, `"tries": {"type": "integer"}`,
			[]string{"$.actions[file.copy].schema.properties.retry: removed"}},
		{"field retyped", `"retry": {"type": "integer"}`, `"retry": {"type": "string"}`,
			[]string{"$.actions[file.copy].schema.properties.retry.type: changed from integer to string"}},
		{"value type changed", `"default": "fail"`, `"default": 1`,
			[]string{"$.actions[file.copy].schema.properties.ifExists.default: type changed from string to number"}},
		{"default changed", `"default": "fail"`, `"default": "replace"`,
			[]string{"$.actions[file.copy].schema.properties.ifExists.default: changed from fail to replace"}},
		{"default removed", `, "default": "fail"`, ``,
			[]string{"$.actions[file.copy].schema.properties.ifExists.default: removed"}},
		{"enum value removed", `"skip", "fail"]`, `"fail"]`,
			[]string{"$.actions[file.copy].schema.properties.ifExists.enum: values removed: skip"}},
		{"field now required", `"required": ["source"]`, `"required": ["source", "retry"]`,
			[]string{"$.actions[file.copy].schema.required: fields now required: retry"}},
		{"type removed", `{"type": "file.move", "label": "Move"}`, `{"type": "file.moved", "label": "Move"}`, []string{"$.actions[file.move]: removed"}},
		{"output retyped", `"type": "list"}`, `"type": "object"}`,
			[]string{"$.actions[file.copy].outputs[output.files].type: changed from list to object"}},
		{"function removed", `, {"name": "upper", "type": "string"}]`, `]`, []string{"$.functions[upper#2]: removed"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cur := strings.Replace(base, tt.from, tt.to, 1)
			if cur == base && tt.from != "" {
				t.Fatalf("%q not in the base", tt.from)
			}
			var o, c any
			if err := json.Unmarshal([]byte(base), &o); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(cur), &c); err != nil {
				t.Fatal(err)
			}
			if got := contractBreaks(o, c); !slices.Equal(got, tt.want) {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}
