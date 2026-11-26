package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func triggerProject(t *testing.T, triggers map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"project.yaml":                "kind: Project\napiVersion: runnel/v1\nname: p\n",
		"workflows/job.workflow.yaml": "kind: Workflow\napiVersion: runnel/v1\nname: job\nactions:\n  - { id: start, type: core.start }\n",
	}
	for name, content := range triggers {
		files["triggers/"+name] = content
	}
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const cliTrigger = "kind: Trigger\napiVersion: runnel/v1\nname: nightly\ntype: cron\ntarget: workflows/job.workflow.yaml\nwith:\n  schedule: \"0 2 * * *\"\n  timezone: UTC\n"

func TestTriggersCommand(t *testing.T) {
	dir := triggerProject(t, map[string]string{
		"nightly.yaml": cliTrigger,
		"off.yaml":     strings.Replace(cliTrigger, "name: nightly", "name: off", 1) + "enabled: false\n",
	})
	code, out, errOut := run(t, "triggers", dir)
	if code != 0 || errOut != "" {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"NAME", "nightly  cron  0 2 * * * (UTC)  workflows/job.workflow.yaml  20", "off", "disabled"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	// Problems are reported like by validate, with exit code 1.
	dir = triggerProject(t, map[string]string{"bad.yaml": strings.Replace(cliTrigger, `"0 2 * * *"`, `"0 2 * *"`, 1)})
	code, out, _ = run(t, "triggers", dir)
	if code != 1 || !strings.Contains(out, `bad.yaml:7:13: error: invalid schedule "0 2 * *": expected exactly 5 fields, found 4`) || !strings.Contains(out, "No triggers") {
		t.Errorf("exit code %d:\n%s", code, out)
	}
}

func TestValidateTriggers(t *testing.T) {
	dir := triggerProject(t, map[string]string{
		"ok.yaml":      cliTrigger,
		"same.yaml":    cliTrigger,
		"missing.yaml": strings.Replace(strings.Replace(cliTrigger, "name: nightly", "name: missing", 1), "job.workflow.yaml", "gone.workflow.yaml", 1),
		// Subfolders of triggers/ are checked too (E12).
		"nightly/again.yaml": cliTrigger,
	})
	code, out, _ := run(t, "validate", dir)
	if code != 1 {
		t.Errorf("exit code %d", code)
	}
	for _, want := range []string{
		"missing.yaml:5:9: error: target workflows/gone.workflow.yaml not found in the project",
		// The first by path keeps the name: nightly/again.yaml sorts before ok.yaml.
		`ok.yaml:3:7: error: trigger name "nightly" is already used by nightly/again.yaml`,
		`same.yaml:3:7: error: trigger name "nightly" is already used by nightly/again.yaml`,
		"1 workflow file and 4 trigger files checked: 3 errors, 0 warnings",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// TestTriggersNoProject: without a project nothing is listed; exit code 2 (R5a), not 1
// for problems in trigger files.
func TestTriggersNoProject(t *testing.T) {
	code, _, errOut := run(t, "triggers", t.TempDir())
	if code != exitUsage || !strings.Contains(errOut, "no project (project.yaml) found") {
		t.Errorf("exit code %d, stderr %q", code, errOut)
	}
}
