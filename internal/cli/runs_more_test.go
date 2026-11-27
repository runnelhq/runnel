package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/history"
	"github.com/runnelhq/runnel/internal/runs"
)

// cols returns the columns of a line of the table of runs without the start time.
func cols(line string) string {
	f := strings.Fields(line)
	if len(f) < 3 {
		return line
	}
	return strings.Join(append(f[:1:1], f[3:]...), " ")
}

// seedHistory writes runs into the history of the project in dir, as the server does.
func seedHistory(t *testing.T, dir, hist string, list []history.Run, logs map[string][]history.LogLine) {
	t.Helper()
	ctx := context.Background()
	hdir, err := history.ProjectDir(hist, dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := history.Open(ctx, hdir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	for _, r := range list {
		if err := s.PutRun(ctx, r); err != nil {
			t.Fatal(err)
		}
		if err := s.AddLog(ctx, r.ID, logs[r.ID]); err != nil {
			t.Fatal(err)
		}
	}
}

// "runnel runs" shows a running run started in the editor (no trigger) with its
// environment, "-" as duration, a log that lost its oldest lines and every level; it
// says when no run matches and how to get the next page.
func TestRunsOutput(t *testing.T) {
	dir, hist := historyProject(t, "")
	started := time.Now().Add(-time.Second)
	finished := started.Add(1500 * time.Millisecond)
	const running, done = "01KB0000000000000000000002", "01KB0000000000000000000001"
	data := func(id, trigger string) []byte {
		b, _ := json.Marshal(runs.Run{ID: id, Path: "workflows/ok.workflow.yaml", Kind: "workflow", Name: "ok", Environment: "dev",
			Trigger: trigger, Status: runs.StatusRunning, StartedAt: started})
		return b
	}
	at := started
	seedHistory(t, dir, hist, []history.Run{
		{ID: done, Path: "workflows/ok.workflow.yaml", Kind: "workflow", Trigger: "cron:nightly", Status: "succeeded", Started: started, Finished: &finished, Data: data(done, "cron:nightly")},
		{ID: running, Path: "workflows/ok.workflow.yaml", Kind: "workflow", Status: "running", Started: started, Data: data(running, "")},
	}, map[string][]history.LogLine{running: {
		{Seq: 4, Time: at, Level: "warn", ActionID: "say", Message: "slow"},
		{Seq: 5, Time: at, Level: "debug", Message: "detail"},
		{Seq: 6, Time: at, Level: "error", Message: "bad"},
	}})

	code, out, errOut := run(t, "runs", "--history-dir", hist, dir)
	if code != 0 {
		t.Fatalf("%d %s", code, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || cols(lines[1]) != running+" running - editor workflows/ok.workflow.yaml" ||
		cols(lines[2]) != done+" succeeded 1.5s cron:nightly workflows/ok.workflow.yaml" {
		t.Errorf("table:\n%s", out)
	}

	code, out, _ = run(t, "runs", "--history-dir", hist, dir, running)
	for _, want := range []string{
		"Run " + running + ` of workflows/ok.workflow.yaml (workflow "ok")`,
		"  running, started ", ", duration -, environment dev\n",
		"  (older lines dropped, 3 kept)\n",
		" WARN  [say] slow\n", " DEBUG detail\n", " ERROR bad\n",
	} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("detail: want %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "trigger") {
		t.Errorf("detail of a run without trigger names one:\n%s", out)
	}

	code, out, _ = run(t, "runs", "--history-dir", hist, "--json", dir, done)
	var d runs.Detail
	if err := json.Unmarshal([]byte(out), &d); code != 0 || err != nil || d.ID != done || d.Status != "succeeded" || d.DurationMs != 1500 || d.Log == nil {
		t.Errorf("detail as JSON: %d %v %s", code, err, out)
	}

	if code, out, _ = run(t, "runs", "--history-dir", hist, "--limit", "1", dir); code != 0 || !strings.HasSuffix(out, "More with --before "+running+"\n") {
		t.Errorf("page: %d\n%s", code, out)
	}
	if code, out, _ = run(t, "runs", "--history-dir", hist, "--status", "failed", dir); code != 0 || out != "No runs in the history of "+displayPath(dir)+" match\n" {
		t.Errorf("no match: %d %q", code, out)
	}
	if code, _, errOut = run(t, "runs", "--history-dir", hist, dir, "01KB0000000000000000000009"); code != 1 || !strings.Contains(errOut, "run 01KB0000000000000000000009 not found in the history of") {
		t.Errorf("unknown id: %d %q", code, errOut)
	}
}

// Without a history: --json lists no runs; an id is not found; a folder without a
// project is an error. Without --history-dir the user's folder is read (TestMain).
func TestRunsWithoutHistory(t *testing.T) {
	dir, hist := historyProject(t, "")
	code, out, _ := run(t, "runs", "--history-dir", hist, "--json", dir)
	var list map[string][]any
	if err := json.Unmarshal([]byte(out), &list); code != 0 || err != nil || list["runs"] == nil || len(list["runs"]) != 0 {
		t.Errorf("json: %d %q %v", code, out, err)
	}
	if code, out, _ := run(t, "runs", dir); code != 0 || !strings.HasPrefix(out, "No runs in the history of ") {
		t.Errorf("default folder: %d %q", code, out)
	}
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"runs", "--history-dir", hist, dir, "01KB0000000000000000000009"}, "the project has no run history in "},
		{[]string{"runs", "--history-dir", hist, t.TempDir()}, "no project (project.yaml) found"},
		{[]string{"runs", "--history-dir", hist, filepath.Join(dir, "workflows")}, ""},
	} {
		code, _, errOut := run(t, tt.args...)
		if tt.want == "" {
			if code != 0 {
				t.Errorf("%v: %d %s", tt.args, code, errOut)
			}
			continue
		}
		if code != 1 || !strings.Contains(errOut, tt.want) {
			t.Errorf("%v: %d %q, want %q", tt.args, code, errOut, tt.want)
		}
	}
}
