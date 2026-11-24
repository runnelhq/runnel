package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/history"
	"github.com/runnelhq/runnel/internal/runs"
)

// After a restart, GET /runs and GET /runs/{id} show the runs from the history
// (ADR 0026); a server without history keeps runs in memory only.
func TestRunsAfterRestart(t *testing.T) {
	dir, _ := newRunProject(t)
	base := t.TempDir()
	var logs []string
	open := func() *Server {
		s, err := New(Options{Dir: dir, Registry: actions.Builtin(), UI: fstest.MapFS{}, HistoryDir: base, Log: func(m string) { logs = append(logs, m) }})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := open()
	run := startRun(t, s, `{"path": "workflows/wait.workflow.yaml", "environment": "dev"}`)
	if _, ok := s.runs.Wait(context.Background(), run.ID); !ok {
		t.Fatal("run not found")
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	want, _ := history.ProjectDir(base, dir)
	if len(logs) == 0 || logs[0] != "run history: "+filepath.Join(want, history.FileName) {
		t.Errorf("logs %v", logs)
	}

	s = open()
	t.Cleanup(func() { _ = s.Shutdown(context.Background()); _ = s.Close() })
	rec := do(t, s, "GET", "/api/v1/runs", "")
	var list struct{ Runs []runs.Run }
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || rec.Code != http.StatusOK {
		t.Fatal(rec.Code, err)
	}
	if len(list.Runs) != 1 || list.Runs[0].ID != run.ID || list.Runs[0].Status != "succeeded" || list.Runs[0].Environment != "dev" {
		t.Fatalf("runs %+v", list.Runs)
	}
	d := getRun(t, s, run.ID)
	if len(d.Actions) != 3 || d.Actions[2].Status != "succeeded" || !strings.Contains(logText(d), "done in dev") {
		t.Errorf("detail %+v %+v", d.Actions, d.Log)
	}
	if rec := do(t, s, "GET", "/api/v1/runs/01NOTTHERE", ""); rec.Code != http.StatusNotFound {
		t.Errorf("missing run: %d", rec.Code)
	}
}

// A history that cannot be opened leaves the server running with runs in memory, and
// says so in the server log.
func TestHistoryOff(t *testing.T) {
	dir, _ := newRunProject(t)
	blocked := filepath.Join(t.TempDir(), "file")
	writeFile(t, blocked, "not a folder")
	var warnings []string
	s, err := New(Options{Dir: dir, Registry: actions.Builtin(), UI: fstest.MapFS{}, HistoryDir: blocked, Warn: func(m string) { warnings = append(warnings, m) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()); _ = s.Close() })
	if len(warnings) != 1 || !strings.Contains(warnings[0], "the run history is off, runs are kept in memory only") {
		t.Errorf("warnings %v", warnings)
	}
	run := startRun(t, s, `{"path": "workflows/wait.workflow.yaml", "environment": "dev"}`)
	if d, ok := s.runs.Wait(context.Background(), run.ID); !ok || d.Status != "succeeded" {
		t.Errorf("run %+v", d.Run)
	}
}

func logText(d runs.Detail) string {
	var b strings.Builder
	for _, l := range d.Log {
		b.WriteString(l.Message + "\n")
	}
	return b.String()
}

// GET /runs filters by status, path and trigger and pages with before and limit (S26b).
func TestRunsFilter(t *testing.T) {
	dir, _ := newRunProject(t)
	s, err := New(Options{Dir: dir, Registry: actions.Builtin(), UI: fstest.MapFS{}, HistoryDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()); _ = s.Close() })
	var ids []string
	for _, body := range []string{`{"path": "workflows/wait.workflow.yaml", "environment": "dev"}`, `{"path": "workflows/wait.workflow.yaml"}`,
		`{"path": "workflows/import.workflow.yaml", "environment": "dev"}`} {
		run := startRun(t, s, body)
		s.runs.Wait(context.Background(), run.ID)
		ids = append(ids, run.ID)
	}
	list := func(query string) string {
		t.Helper()
		rec := do(t, s, "GET", "/api/v1/runs"+query, "")
		var l struct{ Runs []runs.Run }
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &l) != nil {
			t.Fatalf("%s: %d %s", query, rec.Code, rec.Body)
		}
		var out []string
		for _, r := range l.Runs {
			out = append(out, r.ID)
		}
		return strings.Join(out, ",")
	}
	for query, want := range map[string]string{
		"":                                   ids[2] + "," + ids[1] + "," + ids[0],
		"?path=workflows/wait.workflow.yaml": ids[1] + "," + ids[0],
		"?status=failed":                     ids[1], // TARGET is missing without environment
		"?limit=1&before=" + ids[2]:          ids[1],
		"?trigger=cron:x":                    "",
		"?status=succeeded&path=workflows/x.yaml": "",
	} {
		if got := list(query); got != want {
			t.Errorf("%q: %s, want %s", query, got, want)
		}
	}
	for _, q := range []string{"?limit=0", "?limit=x", "?limit=1001"} {
		if rec := do(t, s, "GET", "/api/v1/runs"+q, ""); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid limit") {
			t.Errorf("%s: %d %s", q, rec.Code, rec.Body)
		}
	}
}

// The server removes old runs from the history when it starts, with the limits of
// project.yaml (S26c), and says so in the server log.
func TestPruneAtStart(t *testing.T) {
	dir, _ := newRunProject(t)
	writeFile(t, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: demo\nsettings:\n  history:\n    keepDays: 10\n")
	base := t.TempDir()
	hdir, _ := history.ProjectDir(base, dir)
	store, err := history.Open(context.Background(), hdir)
	if err != nil {
		t.Fatal(err)
	}
	for id, age := range map[string]time.Duration{"01OLD": 11 * 24 * time.Hour, "01NEW": time.Hour} {
		if err := store.PutRun(context.Background(), history.Run{ID: id, Path: "workflows/wait.workflow.yaml", Kind: "workflow",
			Status: "succeeded", Started: time.Now().Add(-age), Data: []byte(`{"id":"` + id + `"}`)}); err != nil {
			t.Fatal(err)
		}
	}
	_ = store.Close()

	var mu sync.Mutex
	var logs []string
	s, err := New(Options{Dir: dir, Registry: actions.Builtin(), UI: fstest.MapFS{}, HistoryDir: base,
		Log: func(m string) { mu.Lock(); logs = append(logs, m); mu.Unlock() }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()); _ = s.Close() })
	removed := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return slices.Contains(logs, "run history: removed 1 old runs (keepDays 10, maxRuns 10000)")
	}
	deadline := time.Now().Add(5 * time.Second)
	for !removed() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !removed() {
		t.Fatalf("logs %v", logs)
	}
	list, err := s.runs.Runs(context.Background())
	if err != nil || len(list) != 1 || list[0].ID != "01NEW" {
		t.Errorf("%+v %v", list, err)
	}
}
