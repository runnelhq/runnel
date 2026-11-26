package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/version"
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
		{"catalog-sftp.golden", []string{"catalog", "sftp"}},
		{"catalog-expressions.golden", []string{"catalog", "--expressions"}},
		{"catalog-expressions.golden.json", []string{"catalog", "--expressions", "--json"}},
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
	if code != 1 || !strings.Contains(errOut, `unknown action, transform or connection type "core.copy"`) {
		t.Errorf("exit code %d, stderr %q", code, errOut)
	}
}

// Every built-in connection type is in the reference with its fields (roadmap E15a).
func TestCatalogJSONConnectionTypes(t *testing.T) {
	code, out, _ := run(t, "catalog", "--json")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	var ref struct {
		Version         string `json:"version"`
		ConnectionTypes []struct {
			Type, Label, Description string
			Schema                   *struct {
				Type       string         `json:"type"`
				Properties map[string]any `json:"properties"`
			} `json:"schema"`
		} `json:"connectionTypes"`
	}
	if err := json.Unmarshal([]byte(out), &ref); err != nil {
		t.Fatal(err)
	}
	if ref.Version != version.Version {
		t.Errorf("version %q, want %q", ref.Version, version.Version)
	}
	var got []string
	for _, c := range ref.ConnectionTypes {
		got = append(got, c.Type)
		if c.Label == "" || c.Description == "" || c.Schema == nil || c.Schema.Type != "object" || len(c.Schema.Properties) == 0 {
			t.Errorf("connection type %s: incomplete %+v", c.Type, c)
		}
	}
	if want := connections.Builtin().Types(); !slices.Equal(got, want) {
		t.Errorf("connection types %v, want %v", got, want)
	}
}

func TestCatalogExpressionsWithType(t *testing.T) {
	code, _, errOut := run(t, "catalog", "--expressions", "core.log")
	if code != 1 || !strings.Contains(errOut, "--expressions takes no type") {
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
