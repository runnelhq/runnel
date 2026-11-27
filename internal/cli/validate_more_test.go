package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// Files that are no valid YAML are reported with their position, each kind of file on
// its own; the other files are still checked. Without arguments the current folder is
// checked.
func TestValidateBrokenYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: b\n")
	writeFile(t, filepath.Join(dir, "workflows", "ok.workflow.yaml"), "kind: Workflow\napiVersion: runnel/v1\nname: ok\nactions:\n  - { id: start, type: core.start }\n")
	writeFile(t, filepath.Join(dir, "workflows", "w.workflow.yaml"), "kind: Workflow\napiVersion: runnel/v1\nname: [\n")
	writeFile(t, filepath.Join(dir, "pipelines", "p.pipeline.yaml"), "kind: Pipeline\r\napiVersion: runnel/v1\r\nname: p\r\ntransforms: {\r\n")
	writeFile(t, filepath.Join(dir, "triggers", "t.yaml"), "kind: Trigger\napiVersion: runnel/v1\nname: t\nschedule: [\n")
	t.Chdir(dir)
	code, out, errOut := run(t, "validate")
	if code != 1 || errOut != "" {
		t.Fatalf("exit code %d, stderr %q\n%s", code, errOut, out)
	}
	for _, want := range []string{
		"w.workflow.yaml:3:",
		"p.pipeline.yaml:4:",
		"t.yaml:4:",
		"2 workflow files, 1 pipeline file and 1 trigger file checked: 3 errors",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}
