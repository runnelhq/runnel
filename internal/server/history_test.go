package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

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
