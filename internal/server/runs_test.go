package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/runs"
)

const waitWorkflow = `kind: Workflow
apiVersion: runnel/v1
name: wait
parameters:
  - { name: DELAY, default: 10ms }
actions:
  - { id: start, type: core.start }
  - { id: wait, type: core.wait, with: { duration: "${DELAY}" } }
  - { id: done, type: core.log, with: { message: "done in ${TARGET}" } }
links:
  - { from: start, to: wait }
  - { from: wait, to: done }
`

func newRunProject(t *testing.T) (string, *Server) {
	t.Helper()
	dir, s := newProject(t)
	writeFile(t, filepath.Join(dir, "workflows", "wait.workflow.yaml"), waitWorkflow)
	return dir, s
}

func startRun(t *testing.T, s *Server, body string) runs.Run {
	t.Helper()
	rec := do(t, s, "POST", "/api/v1/runs", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("start: status %d: %s", rec.Code, rec.Body)
	}
	var run runs.Run
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if rec.Header().Get("Location") != "/api/v1/runs/"+run.ID {
		t.Errorf("Location %q", rec.Header().Get("Location"))
	}
	return run
}

func getRun(t *testing.T, s *Server, id string) runs.Detail {
	t.Helper()
	var d runs.Detail
	rec := do(t, s, "GET", "/api/v1/runs/"+id, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	return d
}

func waitRun(t *testing.T, s *Server, id string, cond func(runs.Detail) bool) runs.Detail {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if d := getRun(t, s, id); cond(d) {
			return d
		} else if time.Now().After(deadline) {
			t.Fatalf("condition not reached: %+v", d.Run)
		}
	}
}

func TestRunLifecycle(t *testing.T) {
	_, s := newRunProject(t)
	run := startRun(t, s, `{"path": "workflows/wait.workflow.yaml", "environment": "dev"}`)
	if run.Status != runs.StatusRunning || run.Environment != "dev" || run.Workflow != "wait" {
		t.Errorf("run %+v", run)
	}
	d := waitRun(t, s, run.ID, func(d runs.Detail) bool { return d.Status != runs.StatusRunning })
	if d.Status != "succeeded" || len(d.Actions) != 3 || d.Log[len(d.Log)-1].Message != "done in dev" {
		t.Errorf("run %+v, log %+v", d.Run, d.Log)
	}
	expect(t, do(t, s, "GET", "/api/v1/runs", ""), http.StatusOK, run.ID)
	expect(t, do(t, s, "POST", "/api/v1/runs/"+run.ID+"/cancel", ""), http.StatusConflict, "already finished")
	expect(t, do(t, s, "GET", "/api/v1/runs/nope", ""), http.StatusNotFound, "run not found")
	expect(t, do(t, s, "POST", "/api/v1/runs/nope/cancel", ""), http.StatusNotFound, "run not found")
}

func TestRunCancel(t *testing.T) {
	_, s := newRunProject(t)
	run := startRun(t, s, `{"path": "workflows/wait.workflow.yaml", "params": {"DELAY": "1h"}}`)
	waitRun(t, s, run.ID, func(d runs.Detail) bool { return d.Actions[1].Status == runs.StatusRunning })
	expect(t, do(t, s, "POST", "/api/v1/runs/"+run.ID+"/cancel", ""), http.StatusAccepted, "cancelling")
	d := waitRun(t, s, run.ID, func(d runs.Detail) bool { return d.Status != runs.StatusRunning })
	if d.Status != "cancelled" {
		t.Errorf("status %s", d.Status)
	}
}

func TestRunStartErrors(t *testing.T) {
	dir, s := newRunProject(t)
	writeFile(t, filepath.Join(dir, "workflows", "broken.workflow.yaml"), "kind: Workflow\napiVersion: runnel/v1\nname: broken\nactions:\n  - { id: start, type: core.start }\n  - { id: x, type: file.shred }\nlinks:\n  - { from: start, to: x }\n")
	rec := do(t, s, "POST", "/api/v1/runs", `{"path": "workflows/broken.workflow.yaml"}`)
	expect(t, rec, http.StatusUnprocessableEntity, `"path": "$.actions[1].type"`)
	expect(t, rec, http.StatusUnprocessableEntity, "cannot be started")

	expect(t, do(t, s, "POST", "/api/v1/runs", `{"path": "workflows/wait.workflow.yaml", "environment": "qa"}`), http.StatusBadRequest, `environment \"qa\" not found`)
	expect(t, do(t, s, "POST", "/api/v1/runs", `{"path": "workflows/wait.workflow.yaml", "params": {"DELAY": 5}}`), http.StatusBadRequest, "invalid request")
	expect(t, do(t, s, "POST", "/api/v1/runs", `{"path": "data/in.csv"}`), http.StatusBadRequest, "only workflow files")
	expect(t, do(t, s, "POST", "/api/v1/runs", `{"path": "workflows/none.workflow.yaml"}`), http.StatusNotFound, "file not found")

	if err := s.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	expect(t, do(t, s, "POST", "/api/v1/runs", `{"path": "workflows/wait.workflow.yaml"}`), http.StatusServiceUnavailable, "shutting down")
}

// sseEvent is one event of a server-sent event stream.
type sseEvent struct {
	id, event, data string
}

// readEvents reads events from a stream until n events of kinds other than ping arrived.
func readEvents(t *testing.T, sc *bufio.Scanner, n int) []sseEvent {
	t.Helper()
	var out []sseEvent
	var cur sseEvent
	for len(out) < n && sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if cur.event != "" {
				out = append(out, cur)
			}
			cur = sseEvent{}
		case strings.HasPrefix(line, "id: "):
			cur.id = line[4:]
		case strings.HasPrefix(line, "event: "):
			cur.event = line[7:]
		case strings.HasPrefix(line, "data: "):
			cur.data = line[6:]
		}
	}
	if len(out) < n {
		t.Fatalf("stream ended after %d events: %v", len(out), sc.Err())
	}
	return out
}

func openStream(t *testing.T, url string, header ...string) *bufio.Scanner {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), "GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("status %d, content type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	return bufio.NewScanner(resp.Body)
}

func TestEvents(t *testing.T) {
	_, s := newRunProject(t)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	stream := openStream(t, ts.URL+"/api/v1/events")
	run := startRun(t, s, `{"path": "workflows/wait.workflow.yaml", "environment": "dev"}`)
	// run.started, start (started, finished), wait (started, log, finished), done (started, log, finished), run.finished
	events := readEvents(t, stream, 10)
	if events[0].event != "run.started" || events[0].id != "1" || events[9].event != "run.finished" {
		t.Errorf("events %v", events)
	}
	var last runs.Event
	if err := json.Unmarshal([]byte(events[9].data), &last); err != nil || last.RunID != run.ID || last.Status != "succeeded" {
		t.Errorf("last event %+v, err %v", last, err)
	}

	// A reconnecting client receives what it missed.
	replay := readEvents(t, openStream(t, ts.URL+"/api/v1/events", "Last-Event-ID", "8"), 2)
	if replay[0].id != "9" || replay[1].event != "run.finished" {
		t.Errorf("replay %v", replay)
	}
	// Filter by run.
	other := startRun(t, s, `{"path": "workflows/wait.workflow.yaml"}`)
	filtered := readEvents(t, openStream(t, ts.URL+"/api/v1/events?since=0&run="+other.ID), 1)
	if !strings.Contains(filtered[0].data, other.ID) {
		t.Errorf("filtered %v", filtered)
	}
	expect(t, do(t, s, "GET", "/api/v1/events?since=x", ""), http.StatusBadRequest, "invalid event id")
}

func TestEventsResetAndShutdown(t *testing.T) {
	_, s := newRunProject(t)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	for range runs.MaxEvents/10 + 1 {
		run := startRun(t, s, `{"path": "workflows/wait.workflow.yaml", "environment": "dev", "params": {"DELAY": "0s"}}`)
		waitRun(t, s, run.ID, func(d runs.Detail) bool { return d.Status != runs.StatusRunning })
	}
	stream := openStream(t, ts.URL+"/api/v1/events", "Last-Event-ID", "0")
	if ev := readEvents(t, stream, 1); ev[0].event != "reset" {
		t.Errorf("first event %v, want reset", ev)
	}

	// Shutdown ends open streams, so that the HTTP server can stop.
	done := make(chan struct{})
	go func() {
		for stream.Scan() {
		}
		close(done)
	}()
	if err := s.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("event stream still open after shutdown")
	}
}
