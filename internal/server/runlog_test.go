package server

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/logfile"
)

type syncWriter struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}

// Each run that ended gets a line in the server log with the fields tools filter on
// (S71); failed runs as warning, line breaks in errors stay on the line.
func TestRunLogLines(t *testing.T) {
	dir, _ := newRunProject(t)
	writeFile(t, filepath.Join(dir, "workflows", "fail.workflow.yaml"), "kind: Workflow\napiVersion: runnel/v1\nname: fail\n"+
		"actions:\n  - { id: start, type: core.start }\n  - { id: stop, type: core.abort, with: { message: \"no\\ndata\" } }\n"+
		"links:\n  - { from: start, to: stop }\n")
	var out syncWriter
	logger := slog.New(logfile.NewHandler(&out, logfile.Options{TimeLayout: "15:04:05"}))
	s, err := New(Options{Dir: dir, Registry: actions.Builtin(), UI: fstest.MapFS{}, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()); _ = s.Close() })
	ok := startRun(t, s, `{"path": "workflows/wait.workflow.yaml", "environment": "dev"}`)
	s.runs.Wait(context.Background(), ok.ID)
	failed := startRun(t, s, `{"path": "workflows/fail.workflow.yaml"}`)
	s.runs.Wait(context.Background(), failed.ID)

	var lines []string
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if lines = strings.Split(strings.TrimSpace(out.String()), "\n"); len(lines) == 2 {
			break
		}
	}
	if len(lines) != 2 {
		t.Fatalf("log:\n%s", out.String())
	}
	if !strings.Contains(lines[0], " INFO  run succeeded run="+ok.ID+" path=workflows/wait.workflow.yaml status=succeeded durationMs=") {
		t.Errorf("line 1: %s", lines[0])
	}
	if !strings.Contains(lines[1], " WARN  run failed run="+failed.ID+" path=workflows/fail.workflow.yaml status=failed") ||
		!strings.Contains(lines[1], `error=no\ndata`) {
		t.Errorf("line 2: %s", lines[1])
	}
}
