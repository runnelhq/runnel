package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/model"
)

// The golden files show the help the editor gives for project.yaml, environments and
// triggers (S58); review their diff whenever a field or its description changes.
func TestSchema(t *testing.T) {
	for _, kind := range model.FileFormats() {
		t.Run(kind, func(t *testing.T) {
			code, out, errOut := run(t, "schema", kind)
			if code != 0 || errOut != "" {
				t.Fatalf("exit code %d, stderr %q", code, errOut)
			}
			compareGolden(t, "schema-"+kind+".golden.json", out)
			// Every field of a file has a description: the editor shows it as help.
			var s map[string]any
			if err := json.Unmarshal([]byte(out), &s); err != nil {
				t.Fatal(err)
			}
			undescribed(t, "", s)
		})
	}
	code, _, errOut := run(t, "schema", "pipeline-x")
	if code == 0 || !strings.Contains(errOut, `unknown file kind "pipeline-x" (expected connection, environment, project, trigger)`) {
		t.Errorf("unknown kind: exit code %d, %s", code, errOut)
	}
	// The kind is not case-sensitive, as in kind: Project.
	if code, _, _ := run(t, "schema", "Project"); code != 0 {
		t.Errorf("Project: exit code %d", code)
	}
}

func undescribed(t *testing.T, path string, s map[string]any) {
	t.Helper()
	props, _ := s["properties"].(map[string]any)
	for name, p := range props {
		ps := p.(map[string]any)
		if d, _ := ps["description"].(string); d == "" {
			t.Errorf("field %s%s has no description", path, name)
		}
		undescribed(t, path+name+".", ps)
	}
}
