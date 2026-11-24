package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/runs"
)

// project writes a small project with a workflow that succeeds and one that fails.
func historyProject(t *testing.T, settings string) (dir, historyDir string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "proj")
	for name, content := range map[string]string{
		"project.yaml": "kind: Project\napiVersion: runnel/v1\nname: proj\n" + settings,
		"workflows/ok.workflow.yaml": "kind: Workflow\napiVersion: runnel/v1\nname: ok\nactions:\n  - { id: start, type: core.start }\n" +
			"  - { id: say, type: core.log, with: { message: hello } }\nlinks:\n  - { from: start, to: say }\n",
		"workflows/bad.workflow.yaml": "kind: Workflow\napiVersion: runnel/v1\nname: bad\nactions:\n  - { id: start, type: core.start }\n" +
			"  - { id: stop, type: core.abort, with: { message: no } }\nlinks:\n  - { from: start, to: stop }\n",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, t.TempDir()
}

func listJSON(t *testing.T, args ...string) []runs.Run {
	t.Helper()
	code, out, errOut := run(t, append([]string{"runs", "--json"}, args...)...)
	if code != 0 {
		t.Fatalf("runs: %d %s", code, errOut)
	}
	var list struct{ Runs []runs.Run }
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return list.Runs
}

// runnel run keeps its runs in the project's history (trigger cli); --history and
// settings.history.cli choose which; runnel runs lists and shows them (S26b).
func TestRunHistory(t *testing.T) {
	dir, hist := historyProject(t, "settings:\n  history:\n    cli: errors\n")
	ok, bad := filepath.Join(dir, "workflows", "ok.workflow.yaml"), filepath.Join(dir, "workflows", "bad.workflow.yaml")
	for _, args := range [][]string{{ok}, {bad}, {"--history", "all", ok}, {"--history", "none", bad}} {
		code, _, errOut := run(t, append([]string{"run", "--history-dir", hist}, args...)...)
		if strings.Contains(errOut, "warning") || (code != 0) != strings.Contains(args[len(args)-1], "bad") {
			t.Fatalf("%v: %d %s", args, code, errOut)
		}
	}
	// errors (project): only bad; --history all: ok; --history none: nothing.
	list := listJSON(t, "--history-dir", hist, dir)
	if len(list) != 2 || list[0].Path != "workflows/ok.workflow.yaml" || list[1].Path != "workflows/bad.workflow.yaml" ||
		list[0].Trigger != "cli" || list[1].Status != "failed" {
		t.Fatalf("runs %+v", list)
	}
	if got := listJSON(t, "--history-dir", hist, "--status", "failed", dir); len(got) != 1 || got[0].ID != list[1].ID {
		t.Errorf("status: %+v", got)
	}
	if got := listJSON(t, "--history-dir", hist, "--limit", "1", "--before", list[0].ID, dir); len(got) != 1 || got[0].ID != list[1].ID {
		t.Errorf("before: %+v", got)
	}

	code, out, _ := run(t, "runs", "--history-dir", hist, dir)
	if code != 0 || !strings.HasPrefix(out, "RUN") || !strings.Contains(out, "succeeded") || !strings.Contains(out, "workflows/bad.workflow.yaml") {
		t.Errorf("table:\n%s", out)
	}
	code, out, _ = run(t, "runs", "--history-dir", hist, dir, list[0].ID)
	if code != 0 || !strings.Contains(out, "Run "+list[0].ID+" of workflows/ok.workflow.yaml") || !strings.Contains(out, "INFO  [say] hello") {
		t.Errorf("detail:\n%s", out)
	}
	t.Chdir(dir) // the id alone: the project is the current folder
	if code, out, _ = run(t, "runs", "--history-dir", hist, list[1].ID); code != 0 || !strings.Contains(out, "Run "+list[1].ID+" of workflows/bad.workflow.yaml") || !strings.Contains(out, "  error: ") {
		t.Errorf("id only:\n%s", out)
	}
}

func TestRunsErrors(t *testing.T) {
	dir, hist := historyProject(t, "")
	// No history yet: nothing is created.
	code, out, _ := run(t, "runs", "--history-dir", hist, dir)
	if entries, _ := os.ReadDir(hist); code != 0 || !strings.Contains(out, "No runs in the history") || len(entries) != 0 {
		t.Errorf("empty: %d %s %v", code, out, entries)
	}
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"runs", "--status", "lost", dir}, `invalid --status "lost"`},
		{[]string{"runs", "--limit", "0", dir}, "invalid --limit 0"},
		{[]string{"runs", "--history-dir", hist, dir, "01M87F7YSRA8YY86KXNHTHGAGV"}, "run 01M87F7YSRA8YY86KXNHTHGAGV not found"},
		{[]string{"run", "--history", "some", filepath.Join(dir, "workflows", "ok.workflow.yaml")}, `invalid --history "some" (expected all, errors or none)`},
	} {
		if code, _, errOut := run(t, tt.args...); code == 0 || !strings.Contains(errOut, tt.want) {
			t.Errorf("%v: %d %s", tt.args, code, errOut)
		}
	}
}

// runnel run removes old runs after its own with settings.history (S26c).
func TestRunPrunesHistory(t *testing.T) {
	dir, hist := historyProject(t, "settings:\n  history:\n    maxRuns: 1\n")
	ok := filepath.Join(dir, "workflows", "ok.workflow.yaml")
	for range 3 {
		if code, _, errOut := run(t, "run", "--history-dir", hist, ok); code != 0 {
			t.Fatal(errOut)
		}
	}
	if list := listJSON(t, "--history-dir", hist, dir); len(list) != 1 {
		t.Errorf("%d runs kept, want 1", len(list))
	}
}
