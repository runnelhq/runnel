package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// An error workflow that is missing or invalid is reported after the failed run, which
// keeps its exit code (ADR 0027).
func TestRunOnErrorNotStarted(t *testing.T) {
	dir, _ := historyProject(t, "settings:\n  onError: workflows/report.workflow.yaml\n")
	bad := filepath.Join(dir, "workflows", "bad.workflow.yaml")
	code, _, errOut := run(t, "run", "--history", "none", bad)
	if code != exitRunFailed || !strings.Contains(errOut, "on error: workflows/report.workflow.yaml could not be started: ") {
		t.Errorf("missing: exit %d, stderr %q", code, errOut)
	}
	writeProjectFile(t, dir, "workflows/report.workflow.yaml", "kind: Workflow\napiVersion: runnel/v1\nname: report\nactions:\n  - { id: x, type: no.such }\n")
	code, _, errOut = run(t, "run", "--history", "none", bad)
	if code != exitRunFailed || !strings.Contains(errOut, "on error: workflows/report.workflow.yaml could not be started: ") || strings.Contains(errOut, "on error: started") {
		t.Errorf("invalid: exit %d, stderr %q", code, errOut)
	}
}

// A pipeline that cannot start (missing parameter, unknown environment, bad flag) ends
// with exit code 2 and prints nothing on stdout but the error workflow's log.
func TestRunPipelineNotStarted(t *testing.T) {
	dir, _ := historyProject(t, "settings:\n  onError: workflows/report.workflow.yaml\n")
	writeProjectFile(t, dir, "workflows/report.workflow.yaml", reportWorkflow)
	pl := writeProjectFile(t, dir, "pipelines/p.pipeline.yaml", "kind: Pipeline\napiVersion: runnel/v1\nname: p\n"+
		"parameters:\n  - { name: N, required: true }\ntransforms:\n  - { id: gen, type: core.generate-rows, with: { count: 1 } }\n")
	code, out, errOut := run(t, "run", "--history", "none", pl)
	if code != exitNotRun || !strings.Contains(errOut, "missing required parameter(s) [N]") || !strings.Contains(errOut, "on error: started workflows/report.workflow.yaml") ||
		!strings.Contains(out, "path=pipelines/p.pipeline.yaml trigger=cli error=missing required parameter(s) [N]") {
		t.Errorf("missing parameter: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"run", "--environment", "qa", "--param", "N=1", pl}, `environment "qa" not found`},
		{[]string{"run", "--history", "some", pl}, `invalid --history "some"`},
	} {
		code, out, errOut := run(t, tt.args...)
		if code != exitNotRun || out != "" || !strings.Contains(errOut, tt.want) {
			t.Errorf("%v: exit %d, stdout %q, stderr %q; want %q", tt.args, code, out, errOut, tt.want)
		}
	}
}

// "runnel serve" checks its log options before it starts.
func TestServeLogOptions(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"serve", "--log-level", "loud", "."}, "--log-level: invalid log level"},
		{[]string{"serve", "--log-format", "xml", "."}, "--log-format: "},
	} {
		code, out, errOut := run(t, tt.args...)
		if code != 1 || out != "" || !strings.Contains(errOut, tt.want) {
			t.Errorf("%v: exit %d, stdout %q, stderr %q; want %q", tt.args, code, out, errOut, tt.want)
		}
	}
}
