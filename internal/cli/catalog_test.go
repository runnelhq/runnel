package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The golden files show what the editor and tools will see of the built-in actions;
// review their diff whenever an action, a label or a field description changes.
func TestCatalog(t *testing.T) {
	tests := []struct {
		golden string
		args   []string
	}{
		{"catalog.golden", []string{"catalog"}},
		{"catalog-log.golden", []string{"catalog", "core.log"}},
		{"catalog-start.golden", []string{"catalog", "core.start"}},
		{"catalog.golden.json", []string{"catalog", "--json"}},
	}
	for _, tt := range tests {
		t.Run(tt.golden, func(t *testing.T) {
			code, out, errOut := run(t, tt.args...)
			if code != 0 || errOut != "" {
				t.Fatalf("exit code %d, stderr %q", code, errOut)
			}
			compareGolden(t, tt.golden, out)
		})
	}
}

func TestCatalogJSONOfOneAction(t *testing.T) {
	code, out, _ := run(t, "catalog", "--json", "core.wait")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	var d struct {
		Type   string `json:"type"`
		Schema struct {
			Required []string `json:"required"`
		} `json:"schema"`
	}
	if err := json.Unmarshal([]byte(out), &d); err != nil || d.Type != "core.wait" || len(d.Schema.Required) != 1 {
		t.Errorf("descriptor %+v, err %v:\n%s", d, err, out)
	}
}

func TestCatalogUnknownType(t *testing.T) {
	code, _, errOut := run(t, "catalog", "core.copy")
	if code != 1 || !strings.Contains(errOut, `unknown action or transform type "core.copy"`) {
		t.Errorf("exit code %d, stderr %q", code, errOut)
	}
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden file (run with -update): %v", err)
	}
	if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Errorf("output differs from %s (run with -update if intended)\n--- got:\n%s\n--- want:\n%s", path, got, want)
	}
}
