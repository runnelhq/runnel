package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const reportWorkflow = "kind: Workflow\napiVersion: runnel/v1\nname: report\nparameters:\n  - { name: FAILED_RUN }\n  - { name: FAILED_PATH }\n" +
	"  - { name: FAILED_TRIGGER }\n  - { name: FAILED_ERROR }\nactions:\n  - { id: start, type: core.start }\n" +
	"  - { id: log, type: core.log, with: { message: \"run=${FAILED_RUN} path=${FAILED_PATH} trigger=${FAILED_TRIGGER} error=${FAILED_ERROR}\" } }\n" +
	"links:\n  - { from: start, to: log }\n"

func writeProjectFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// runnel run starts the error workflow after a failed run in the same process (ADR
// 0027); the exit code stays that of the run, the error run is in the history with
// trigger error and the failed run as parent. --no-on-error turns it off.
func TestRunOnError(t *testing.T) {
	dir, hist := historyProject(t, "settings:\n  onError: workflows/report.workflow.yaml\n")
	writeProjectFile(t, dir, "workflows/report.workflow.yaml", reportWorkflow)
	ok, bad := filepath.Join(dir, "workflows", "ok.workflow.yaml"), filepath.Join(dir, "workflows", "bad.workflow.yaml")

	code, out, errOut := run(t, "run", "--history-dir", hist, bad)
	if code != 1 || !strings.Contains(errOut, "on error: started workflows/report.workflow.yaml (run ") ||
		!strings.Contains(errOut, "on error: workflows/report.workflow.yaml succeeded (run ") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	list := listJSON(t, "--history-dir", hist, dir)
	if len(list) != 2 || list[0].Trigger != "error" || list[0].Parent != list[1].ID || list[1].Path != "workflows/bad.workflow.yaml" {
		t.Fatalf("runs %+v", list)
	}
	if want := "run=" + list[1].ID + " path=workflows/bad.workflow.yaml trigger=cli error=no"; !strings.Contains(out, want) {
		t.Errorf("stdout %q, want %q", out, want)
	}

	for _, args := range [][]string{{"--no-on-error", bad}, {ok}} {
		code, _, errOut := run(t, append([]string{"run", "--history", "none"}, args...)...)
		if strings.Contains(errOut, "on error") || (code == 0) != (args[0] == ok) {
			t.Errorf("%v: exit %d, stderr %q", args, code, errOut)
		}
	}

	// Not started (missing parameter): reported without run id; exit code 2.
	needs := writeProjectFile(t, dir, "workflows/needs.workflow.yaml", "kind: Workflow\napiVersion: runnel/v1\nname: needs\n"+
		"parameters:\n  - { name: X, required: true }\nactions:\n  - { id: start, type: core.start }\n")
	code, out, errOut = run(t, "run", "--history", "none", needs)
	if code != 2 || !strings.Contains(errOut, "on error: started") ||
		!strings.Contains(out, "run= path=workflows/needs.workflow.yaml trigger=cli error=missing required parameter(s) [X]") {
		t.Errorf("not started: exit %d, stdout %q, stderr %q", code, out, errOut)
	}

	// A pipeline with its own error workflow that fails: the exit code stays 1.
	writeProjectFile(t, dir, "workflows/broken.workflow.yaml", "kind: Workflow\napiVersion: runnel/v1\nname: broken\n"+
		"actions:\n  - { id: start, type: core.start }\n  - { id: stop, type: core.abort }\nlinks:\n  - { from: start, to: stop }\n")
	pl := writeProjectFile(t, dir, "pipelines/read.pipeline.yaml", "kind: Pipeline\napiVersion: runnel/v1\nname: read\n"+
		"settings:\n  onError: workflows/broken.workflow.yaml\ntransforms:\n  - { id: read, type: csv.input, with: { path: missing.csv } }\n")
	code, _, errOut = run(t, "run", "--history", "none", pl)
	if code != 1 || !strings.Contains(errOut, "on error: workflows/broken.workflow.yaml failed (run ") {
		t.Errorf("pipeline: exit %d, stderr %q", code, errOut)
	}
}

// validate checks settings.onError against the project with its position (ADR 0027).
func TestValidateOnError(t *testing.T) {
	dir, _ := historyProject(t, "settings:\n  onError: workflows/missing.workflow.yaml\n")
	writeProjectFile(t, dir, "workflows/extra.workflow.yaml", "kind: Workflow\napiVersion: runnel/v1\nname: extra\n"+
		"parameters:\n  - { name: TO, required: true }\n  - { name: FAILED_STARTED, type: integer }\nactions:\n  - { id: start, type: core.start }\n")
	writeProjectFile(t, dir, "workflows/self.workflow.yaml", "kind: Workflow\napiVersion: runnel/v1\nname: self\n"+
		"settings:\n  onError: workflows/self.workflow.yaml\nactions:\n  - { id: start, type: core.start }\n")
	writeProjectFile(t, dir, "workflows/uses.workflow.yaml", "kind: Workflow\napiVersion: runnel/v1\nname: uses\n"+
		"settings:\n  onError: workflows/extra.workflow.yaml\nactions:\n  - { id: start, type: core.start }\n")
	writeProjectFile(t, dir, "pipelines/p.pipeline.yaml", "kind: Pipeline\napiVersion: runnel/v1\nname: p\n"+
		"settings:\n  onError: pipelines/p.pipeline.yaml\ntransforms:\n  - { id: rows, type: core.generate-rows, with: { count: 1 } }\n")
	t.Chdir(dir)
	code, out, _ := run(t, "validate", ".")
	for _, want := range []string{
		"project.yaml:5:12: error: error workflow workflows/missing.workflow.yaml not found",
		"workflows/uses.workflow.yaml:5:12: error: error workflow workflows/extra.workflow.yaml: required parameter TO is not given",
		"workflows/uses.workflow.yaml:5:12: error: error workflow workflows/extra.workflow.yaml: parameter FAILED_STARTED must be of type string, not integer",
		"workflows/self.workflow.yaml:5:12: warning: onError names this workflow itself; it is ignored here",
		"pipelines/p.pipeline.yaml:5:12: error: onError \"pipelines/p.pipeline.yaml\" must be a workflow file",
	} {
		if !strings.Contains(filepath.ToSlash(out), want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if code != 1 {
		t.Errorf("exit %d", code)
	}
}
