package triggers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/runs"
)

const apiTarget = `kind: Workflow
apiVersion: runnel/v1
name: api
parameters:
  - { name: ID, type: integer, required: true }
  - { name: NAME }
  - { name: DATA, type: json }
  - { name: MODE, enum: [fast, slow], default: slow }
actions:
  - { id: start, type: core.start }
`

func httpTrigger(name, method, path, extra string) string {
	return fmt.Sprintf("kind: Trigger\napiVersion: runnel/v1\nname: %s\ntype: http\ntarget: workflows/api.workflow.yaml\n%swith:\n  method: %s\n  path: %s\n", name, extra, method, path)
}

func httpSetup(t *testing.T) (*fake, *Scheduler) {
	t.Helper()
	t.Setenv("RUNNEL_TEST_TOKEN", "s3cret")
	dir, f, s, _ := setup(t, map[string]string{
		"items.yaml": httpTrigger("items", "POST", "/api/items/{ID}", "params: { MODE: fast }\n") + "  auth: { token: \"${env:RUNNEL_TEST_TOKEN}\" }\n",
		"open.yaml":  httpTrigger("open", "GET", "/open", "") + "  auth: none\n  maxConcurrent: 1\n",
		"off.yaml":   httpTrigger("off", "GET", "/off", "enabled: false\n") + "  auth: none\n",
	})
	write(t, dir, "workflows/api.workflow.yaml", apiTarget)
	s.Reload()
	if p := s.Problems(); len(p) != 0 {
		t.Fatal(p)
	}
	return f, s
}

type call struct {
	method, path, contentType, body string
	token, host, origin             string
}

func (c call) do(s *Scheduler) *httptest.ResponseRecorder {
	r := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
	r.Host = "127.0.0.1:7301"
	if c.host != "" {
		r.Host = c.host
	}
	if c.origin != "" {
		r.Header.Set("Origin", c.origin)
	}
	r.RemoteAddr = "192.0.2.7:51234"
	if c.contentType != "" {
		r.Header.Set("Content-Type", c.contentType)
	}
	if c.token != "" {
		r.Header.Set("Authorization", "Bearer "+c.token)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

// Requests are routed by method and path, checked and turned into parameters; the run
// is started with 202 and its id.
func TestHTTPTrigger(t *testing.T) {
	f, s := httpSetup(t)
	w := call{method: "POST", path: "/API/items/4711?NAME=query", contentType: "application/json", token: "s3cret",
		body: `{"ID": 1, "NAME": "body", "DATA": {"a": [1, 2]}, "MODE": "slow", "OTHER": true}`}.do(s)
	var res acceptedBody
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || w.Code != http.StatusAccepted || res.RunID != "run-1" ||
		res.Status != "/api/v1/runs/run-1" || w.Header().Get("Location") != "/api/v1/runs/run-1" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	// Path before query before body; fixed params win; unknown fields are ignored.
	p := f.params[0]
	if p["ID"] != "4711" || p["NAME"] != "query" || p["DATA"] != `{"a":[1,2]}` || p["MODE"] != "fast" || len(p) != 4 || f.clients[0] != "192.0.2.7" {
		t.Errorf("params %v, client %q", p, f.clients[0])
	}
	// A form works like a JSON object.
	w = call{method: "POST", path: "/api/items/5", contentType: "application/x-www-form-urlencoded", token: "s3cret", body: "NAME=Anna+M%C3%BCller"}.do(s)
	if w.Code != http.StatusAccepted || f.params[1]["NAME"] != "Anna Müller" {
		t.Errorf("form: %d %s %v", w.Code, w.Body, f.params[1])
	}
	if last := s.List()[0].Last; last == nil || last.RunID != "run-2" {
		t.Errorf("last %+v", last)
	}

	for _, c := range []struct {
		call
		status int
		want   string
	}{
		{call{method: "GET", path: "/nothing"}, 404, "no trigger for GET /nothing"},
		{call{method: "GET", path: "/open?ID=1", host: "evil.example:7301"}, 403, "requests must be addressed to localhost or 127.0.0.1"},
		{call{method: "GET", path: "/open?ID=1", origin: "https://evil.example"}, 403, "requests from web pages (with an Origin header) are not accepted"},
		{call{method: "GET", path: "/off"}, 404, `trigger \"off\" is disabled`},
		{call{method: "DELETE", path: "/api/items/1"}, 405, "method DELETE is not allowed for /api/items/1 (allowed: POST)"},
		{call{method: "POST", path: "/api/items/1"}, 401, "missing or wrong token"},
		{call{method: "POST", path: "/api/items/1", token: "wrong"}, 401, "missing or wrong token"},
		{call{method: "POST", path: "/api/items/x", token: "s3cret"}, 400, "parameter ID (integer)"},
		{call{method: "POST", path: "/api/items/1", token: "s3cret", contentType: "application/json", body: `{"DATA": "{x"}`}, 400, "parameter DATA (json)"},
		{call{method: "POST", path: "/api/items/1", token: "s3cret", contentType: "application/json", body: `[1, 2]`}, 400, "the body must be a JSON object"},
		{call{method: "POST", path: "/api/items/1", token: "s3cret", contentType: "text/csv", body: "a;b"}, 415, `unsupported content type \"text/csv\"`},
		{call{method: "POST", path: "/api/items/1", token: "s3cret", body: `{"NAME": "` + strings.Repeat("x", MaxBody) + `"}`}, 413, "the body is larger than 1048576 bytes"},
	} {
		w := c.do(s)
		if w.Code != c.status || !strings.Contains(w.Body.String(), c.want) {
			t.Errorf("%s %s: %d %s, want %d %q", c.method, c.path, w.Code, w.Body, c.status, c.want)
		}
	}
	if w.Header().Get("Allow") != "" {
		t.Error("Allow on 202")
	}
	if f.count() != 2 {
		t.Errorf("runs started: %v", f.params)
	}
}

// A missing required parameter is reported before a run starts; maxConcurrent and the
// limits of the project answer 429, a server shutting down 503.
func TestHTTPTriggerLimits(t *testing.T) {
	dir, f, s, _ := setup(t, map[string]string{"open.yaml": httpTrigger("open", "GET", "/open", "") + "  auth: none\n  maxConcurrent: 1\n"})
	write(t, dir, "workflows/api.workflow.yaml", apiTarget)
	s.Reload()
	open := call{method: "GET", path: "/open"}
	if w := open.do(s); w.Code != 400 || !strings.Contains(w.Body.String(), "missing required parameter ID of workflow workflows/api.workflow.yaml") {
		t.Errorf("missing: %d %s", w.Code, w.Body)
	}
	open.path = "/open?ID=1"
	if w := open.do(s); w.Code != 202 {
		t.Fatalf("first: %d %s", w.Code, w.Body)
	}
	if w := open.do(s); w.Code != 429 || !strings.Contains(w.Body.String(), `trigger \"open\" is already running 1 run (maxConcurrent: 1)`) {
		t.Errorf("second: %d %s", w.Code, w.Body)
	}
	f.running = map[string]bool{}
	for _, c := range []struct {
		err    error
		status int
	}{
		{&runs.LimitError{}, 429},
		{runs.ErrShutdown, 503},
		{fmt.Errorf("broken"), 500},
	} {
		f.fail = c.err
		if w := open.do(s); w.Code != c.status {
			t.Errorf("%v: %d %s", c.err, w.Code, w.Body)
		}
	}
	if !strings.Contains(strings.Join(f.logs, "\n"), `trigger "open": workflows/api.workflow.yaml was not started: broken`) {
		t.Errorf("logs %v", f.logs)
	}
}

// The listener opens once an http trigger is enabled and closes when the scheduler ends.
func TestHTTPListener(t *testing.T) {
	_, _, s, _ := setup(t, nil)
	s.opts.Listen = "127.0.0.1:0"
	s.opts.Now = time.Now
	s.listen()
	if s.HTTPAddress() != "" {
		t.Fatal("listening without http triggers")
	}
	_, s = httpSetup(t)
	s.opts.Listen = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	var addr string
	for deadline := time.Now().Add(5 * time.Second); addr == ""; time.Sleep(10 * time.Millisecond) {
		if addr = s.HTTPAddress(); addr == "" && time.Now().After(deadline) {
			t.Fatal("not listening")
		}
	}
	resp, err := http.Get("http://" + addr + "/open?ID=3")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 202 || !strings.Contains(string(body), `"runId":"run-1"`) {
		t.Errorf("%d %s", resp.StatusCode, body)
	}
	cancel()
	<-done
	if s.HTTPAddress() != "" {
		t.Error("still listening")
	}
}
