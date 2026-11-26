package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/runs"
	"github.com/runnelhq/runnel/internal/transforms"
	"github.com/runnelhq/runnel/internal/triggers"
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
	expect(t, do(t, s, "POST", "/api/v1/runs", `{"path": "data/in.csv"}`), http.StatusBadRequest, "only workflow and pipeline files")
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
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
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

// A second start beyond settings.maxConcurrentRuns is rejected with 429 (S37).
func TestRunLimit(t *testing.T) {
	dir, s := newRunProject(t)
	writeFile(t, filepath.Join(dir, "workflows", "once.workflow.yaml"),
		strings.Replace(waitWorkflow, "name: wait\n", "name: once\nsettings: { maxConcurrentRuns: 1 }\n", 1))
	run := startRun(t, s, `{"path": "workflows/once.workflow.yaml", "params": {"DELAY": "1h"}}`)
	expect(t, do(t, s, "POST", "/api/v1/runs", `{"path": "workflows/once.workflow.yaml", "params": {"DELAY": "1h"}}`),
		http.StatusTooManyRequests, "is already running 1 run (settings.maxConcurrentRuns: 1)")
	expect(t, do(t, s, "POST", "/api/v1/runs/"+run.ID+"/cancel", ""), http.StatusAccepted, "cancelling")
}

// GET /triggers lists the triggers with their next time and the problems of trigger files;
// a trigger starts its target with its parameters and records itself on the run.
func TestTriggers(t *testing.T) {
	dir, s := newRunProject(t)
	writeFile(t, filepath.Join(dir, "triggers", "often.yaml"), "kind: Trigger\napiVersion: runnel/v1\nname: often\ntype: cron\n"+
		"target: workflows/wait.workflow.yaml\nenvironment: dev\nparams: { DELAY: 1ms }\nwith: { schedule: \"* * * * *\" }\n")
	writeFile(t, filepath.Join(dir, "triggers", "bad.yaml"), "kind: Trigger\napiVersion: runnel/v1\nname: bad\ntype: http\ntarget: workflows/wait.workflow.yaml\n"+
		"with: { method: GET, path: /bad, auth: none, history: some }\n")
	s.triggers.Reload()
	rec := do(t, s, "GET", "/api/v1/triggers", "")
	var res triggersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if len(res.Triggers) != 1 || res.Triggers[0].Name != "often" || res.Triggers[0].File != "triggers/often.yaml" || res.Triggers[0].Next.IsZero() {
		t.Errorf("triggers %+v", res.Triggers)
	}
	if len(res.Problems) != 1 || res.Problems[0].File != "triggers/bad.yaml" || !strings.Contains(res.Problems[0].Message, "invalid history") {
		t.Errorf("problems %+v", res.Problems)
	}

	s.triggers.Tick(res.Triggers[0].Next)
	list := s.triggers.List()
	if list[0].Last == nil || list[0].Last.RunID == "" {
		t.Fatalf("not started: %+v", list[0].Last)
	}
	d := waitRun(t, s, list[0].Last.RunID, func(d runs.Detail) bool { return d.Status != runs.StatusRunning })
	if d.Status != "succeeded" || d.Trigger != "cron:often" || d.Params["DELAY"] != "1ms" || d.Environment != "dev" {
		t.Errorf("run %+v", d.Run)
	}
}

// A file trigger starts its target with the path of the new file (S38).
func TestFileTrigger(t *testing.T) {
	dir, s := newRunProject(t)
	writeFile(t, filepath.Join(dir, "workflows", "file.workflow.yaml"), "kind: Workflow\napiVersion: runnel/v1\nname: file\n"+
		"parameters:\n  - { name: FILE, required: true }\nactions:\n  - { id: start, type: core.start }\n")
	writeFile(t, filepath.Join(dir, "triggers", "inbox.yaml"), "kind: Trigger\napiVersion: runnel/v1\nname: inbox\ntype: file\n"+
		"target: workflows/file.workflow.yaml\nwith: { folder: inbox, param: FILE }\n")
	writeFile(t, filepath.Join(dir, "inbox", "a.csv"), "x\n")
	s.triggers.Reload()
	if p := s.triggers.Problems(); len(p) != 0 {
		t.Fatal(p)
	}
	now := time.Now()
	s.triggers.Tick(now)
	s.triggers.Tick(now.Add(model.DefaultFileInterval))
	last := s.triggers.List()[0].Last
	if last == nil || last.RunID == "" {
		t.Fatalf("not started: %+v", last)
	}
	d := waitRun(t, s, last.RunID, func(d runs.Detail) bool { return d.Status != runs.StatusRunning })
	want, _ := filepath.Abs(filepath.Join(dir, "inbox", "a.csv"))
	if d.Status != "succeeded" || d.Trigger != "file:inbox" || d.Params["FILE"] != want {
		t.Errorf("run %+v", d.Run)
	}
}

// A request to an http trigger starts its target with the values of the request and
// the address of the caller (S38a).
func TestHTTPTrigger(t *testing.T) {
	dir, s := newRunProject(t)
	writeFile(t, filepath.Join(dir, "triggers", "hook.yaml"), "kind: Trigger\napiVersion: runnel/v1\nname: hook\ntype: http\n"+
		"target: workflows/wait.workflow.yaml\nenvironment: dev\nwith: { method: POST, path: /hooks/wait, auth: none }\n")
	s.triggers.Reload()
	if p := s.triggers.Problems(); len(p) != 0 {
		t.Fatal(p)
	}
	r := httptest.NewRequest(http.MethodPost, "/hooks/wait", strings.NewReader(`{"DELAY": "1ms"}`))
	r.Host, r.RemoteAddr = "127.0.0.1:7301", "127.0.0.1:50000"
	w := httptest.NewRecorder()
	s.triggers.ServeHTTP(w, r)
	var res struct{ RunID string }
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || w.Code != http.StatusAccepted {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	d := waitRun(t, s, res.RunID, func(d runs.Detail) bool { return d.Status != runs.StatusRunning })
	if d.Status != "succeeded" || d.Trigger != "http:hook" || d.Client != "127.0.0.1" || d.Params["DELAY"] != "1ms" {
		t.Errorf("run %+v", d.Run)
	}
}

// With response: run the workflow answers the caller with http.response, also from a
// called workflow; without it the outputs are the answer (S38b1).
func TestHTTPTriggerResponse(t *testing.T) {
	dir, s := newRunProject(t)
	files := map[string]string{
		"workflows/item.workflow.yaml": `kind: Workflow
apiVersion: runnel/v1
name: item
parameters:
  - { name: ID, type: integer, required: true }
actions:
  - { id: start, type: core.start }
  - id: reply
    type: http.response
    with:
      status: "{{ params.ID > 100 ? 404 : 200 }}"
      headers: { X-Method: "{{ request.method }}" }
      body: { id: "{{ params.ID }}", name: "{{ request?.body?.name ?? 'none' }}" }
  - { id: after, type: core.log, with: { message: "answered: {{ results.reply.output.sent }}" } }
links:
  - { from: start, to: reply }
  - { from: reply, to: after }
`,
		"workflows/outer.workflow.yaml": `kind: Workflow
apiVersion: runnel/v1
name: outer
parameters:
  - { name: ID, type: integer, required: true }
actions:
  - { id: start, type: core.start }
  - { id: call, type: core.run-workflow, with: { workflow: workflows/item.workflow.yaml, params: { ID: "${ID}" } } }
links:
  - { from: start, to: call }
`,
		"workflows/sum.workflow.yaml": `kind: Workflow
apiVersion: runnel/v1
name: sum
parameters:
  - { name: A, type: integer, required: true }
outputs:
  - { name: TWICE, type: integer, value: "{{ params.A * 2 }}" }
actions:
  - { id: start, type: core.start }
`,
	}
	for name, content := range files {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(name)), content)
	}
	for _, tr := range []struct{ name, target, path string }{
		{"item", "item", "/items/{ID}"}, {"outer", "outer", "/outer/{ID}"}, {"sum", "sum", "/sum"},
	} {
		writeFile(t, filepath.Join(dir, "triggers", tr.name+".yaml"), "kind: Trigger\napiVersion: runnel/v1\nname: "+tr.name+"\ntype: http\n"+
			"target: workflows/"+tr.target+".workflow.yaml\nwith: { method: POST, path: \""+tr.path+"\", auth: none, response: run, timeout: 5s, history: all }\n")
	}
	s.triggers.Reload()
	if p := s.triggers.Problems(); len(p) != 0 {
		t.Fatal(p)
	}
	post := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Host, r.RemoteAddr = "127.0.0.1:7301", "127.0.0.1:50000"
		w := httptest.NewRecorder()
		s.triggers.ServeHTTP(w, r)
		return w
	}
	for _, c := range []struct {
		path, body string
		status     int
		want       string
	}{
		{"/items/7", `{"name": "Anna"}`, 200, `{"id":7,"name":"Anna"}`},
		{"/items/700", "", 404, `{"id":700,"name":"none"}`},
		{"/outer/8", `{"name": "Ben"}`, 200, `{"id":8,"name":"Ben"}`},
		{"/sum?A=21", "", 200, `{"TWICE":42}`},
	} {
		w := post(c.path, c.body)
		if w.Code != c.status || strings.TrimSpace(w.Body.String()) != c.want {
			t.Errorf("%s: %d %s", c.path, w.Code, w.Body)
		}
		if c.path == "/items/7" && w.Header().Get("X-Method") != "POST" {
			t.Errorf("headers %v", w.Header())
		}
	}
	// The workflow goes on after the answer.
	d := waitRun(t, s, s.triggers.List()[0].Last.RunID, func(d runs.Detail) bool { return d.Status != runs.StatusRunning })
	if !strings.Contains(fmt.Sprint(d.Log), "answered: true") {
		t.Errorf("log %v", d.Log)
	}
}

// A pipeline gets a JSON list in the body as records and answers with http.response
// (S38c).
func TestHTTPTriggerPipeline(t *testing.T) {
	dir, s := newRunProject(t)
	s.transforms = transforms.Builtin()
	writeFile(t, filepath.Join(dir, "pipelines", "big.pipeline.yaml"), `kind: Pipeline
apiVersion: runnel/v1
name: big
parameters:
  - { name: MIN, type: integer, default: "10" }
transforms:
  - { id: in, type: core.from-result }
  - { id: big, type: core.filter, with: { condition: "{{ record.amount >= params.MIN }}" } }
  - { id: answer, type: http.response, with: { status: "200", headers: { X-Kind: orders } } }
  - { id: none, type: http.response, with: { status: "404", whenEmpty: true } }
links:
  - { from: in, to: big }
  - { from: big, to: answer }
  - { from: big, to: none }
`)
	writeFile(t, filepath.Join(dir, "triggers", "big.yaml"), "kind: Trigger\napiVersion: runnel/v1\nname: big\ntype: http\n"+
		"target: pipelines/big.pipeline.yaml\nwith: { method: POST, path: /orders/big, auth: none, response: run, timeout: 5s }\n")
	s.triggers.Reload()
	if p := s.triggers.Problems(); len(p) != 0 {
		t.Fatal(p)
	}
	post := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Host, r.RemoteAddr = "127.0.0.1:7301", "127.0.0.1:50000"
		w := httptest.NewRecorder()
		s.triggers.ServeHTTP(w, r)
		return w
	}
	w := post("/orders/big", `[{"id": 1, "amount": 5}, {"id": 2, "amount": 50}, {"id": 3, "amount": 12.5}]`)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `[{"amount":50,"id":2},{"amount":12.5,"id":3}]` || w.Header().Get("X-Kind") != "orders" {
		t.Errorf("records: %d %s %v", w.Code, w.Body, w.Header())
	}
	if w := post("/orders/big?MIN=100", `[{"id": 1, "amount": 5}]`); w.Code != 404 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Errorf("none: %d %s", w.Code, w.Body)
	}
	// history: errors (the default with response: run) drops runs that succeeded.
	last := s.triggers.List()[0].Last
	if last == nil {
		t.Fatal("no run")
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, ok, _ := s.runs.Get(context.Background(), last.RunID); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the run that succeeded is kept (history: errors)")
		}
	}
}

// The default answer of an http trigger with response: run waits on the started run
// itself: with history errors or none, a run that has already ended and left memory
// when the wait begins is no longer found by its id, and the caller got a 504.
func TestTriggerWaitAfterRunDropped(t *testing.T) {
	_, s := newRunProject(t)
	tr, err := model.ParseTrigger("triggers/api.yaml", []byte("kind: Trigger\napiVersion: runnel/v1\nname: api\ntype: http\n"+
		"target: workflows/wait.workflow.yaml\nenvironment: dev\nwith: { method: POST, path: /api, auth: none, response: run }\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, history := range []string{runs.HistoryErrors, runs.HistoryNone} {
		started, err := s.startTrigger(tr, triggers.StartRequest{Params: map[string]string{"DELAY": "0s"}, History: history})
		if err != nil || started.Wait == nil {
			t.Fatalf("%s: %v", history, err)
		}
		for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
			if _, ok, _ := s.runs.Get(context.Background(), started.ID); !ok {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: the run that succeeded is kept", history)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, ok := started.Wait(ctx)
		cancel()
		if !ok || out.Status != "succeeded" {
			t.Errorf("%s: %v %+v", history, ok, out)
		}
	}
}

// "Run target now" (E20a): POST /runs with a saved trigger starts its target with the
// environment and parameters of the trigger.
func TestRunTriggerTarget(t *testing.T) {
	dir, s := newRunProject(t)
	writeFile(t, filepath.Join(dir, "triggers", "hourly.yaml"), "kind: Trigger\napiVersion: runnel/v1\nname: hourly\ntype: interval\ntarget: workflows/wait.workflow.yaml\nenvironment: dev\nenabled: false\nparams:\n  DELAY: 1ms\nwith:\n  every: 1h\n")
	run := startRun(t, s, `{"trigger": "triggers/hourly.yaml"}`)
	d := waitRun(t, s, run.ID, func(d runs.Detail) bool { return d.Status != runs.StatusRunning })
	if d.Status != "succeeded" || d.Environment != "dev" || d.Trigger != "interval:hourly" || d.Log[len(d.Log)-1].Message != "done in dev" {
		t.Errorf("run %+v, log %+v", d.Run, d.Log)
	}
	// Refused: other fields beside trigger, a file that is not a trigger, a trigger with problems, a missing one.
	expect(t, do(t, s, "POST", "/api/v1/runs", `{"trigger": "triggers/hourly.yaml", "environment": "prod"}`), http.StatusBadRequest, "takes path, environment and params from the trigger")
	expect(t, do(t, s, "POST", "/api/v1/runs", `{"trigger": "workflows/wait.workflow.yaml"}`), http.StatusBadRequest, "must be a trigger file")
	writeFile(t, filepath.Join(dir, "triggers", "broken.yaml"), "kind: Trigger\napiVersion: runnel/v1\nname: broken\ntype: cron\ntarget: workflows/wait.workflow.yaml\n")
	expect(t, do(t, s, "POST", "/api/v1/runs", `{"trigger": "triggers/broken.yaml"}`), http.StatusUnprocessableEntity, `missing required field \"schedule\"`)
	expect(t, do(t, s, "POST", "/api/v1/runs", `{"trigger": "triggers/gone.yaml"}`), http.StatusNotFound, "file not found")
	expect(t, do(t, s, "POST", "/api/v1/runs", `{"trigger": "../x.yaml"}`), http.StatusBadRequest, "")
}
