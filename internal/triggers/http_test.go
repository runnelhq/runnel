package triggers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/runs"
	"github.com/runnelhq/runnel/internal/tlscert"
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
	var res map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || w.Code != http.StatusAccepted || res["runId"] != "run-1" ||
		len(res) != 1 || w.Header().Get("Location") != "/api/v1/runs/run-1" {
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

// Without maxConcurrent an http trigger runs at most 4 runs at the same time (R5a): the
// 5th request is refused with 429. maxConcurrent: 0, set explicitly, has no own limit.
func TestHTTPTriggerDefaultLimit(t *testing.T) {
	dir, f, s, _ := setup(t, map[string]string{
		"open.yaml":      httpTrigger("open", "GET", "/open", "") + "  auth: none\n",
		"unlimited.yaml": httpTrigger("unlimited", "GET", "/unlimited", "") + "  auth: none\n  maxConcurrent: 0\n",
	})
	write(t, dir, "workflows/api.workflow.yaml", apiTarget)
	s.Reload()
	open := call{method: "GET", path: "/open?ID=1"}
	for i := range model.DefaultHTTPMaxConcurrent {
		if w := open.do(s); w.Code != 202 {
			t.Fatalf("request %d: %d %s", i+1, w.Code, w.Body)
		}
	}
	if w := open.do(s); w.Code != 429 || !strings.Contains(w.Body.String(), `trigger \"open\" is already running 4 runs (maxConcurrent: 4)`) {
		t.Errorf("5th: %d %s", w.Code, w.Body)
	}
	unlimited := call{method: "GET", path: "/unlimited?ID=1"}
	for i := range 2 * model.DefaultHTTPMaxConcurrent {
		if w := unlimited.do(s); w.Code != 202 {
			t.Fatalf("unlimited, request %d: %d %s", i+1, w.Code, w.Body)
		}
	}
	if f.count() != 3*model.DefaultHTTPMaxConcurrent {
		t.Errorf("runs started: %d", f.count())
	}
}

// The listener opens once an http trigger is enabled and closes when the scheduler ends.
func TestHTTPListener(t *testing.T) {
	_, _, s, _ := setup(t, nil)
	s.opts.Listen = "127.0.0.1:0"
	s.opts.Now = time.Now
	s.http.listen()
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
	if resp.StatusCode != http.StatusAccepted || !strings.Contains(string(body), `"runId":"run-1"`) {
		t.Errorf("%d %s", resp.StatusCode, body)
	}
	cancel()
	<-done
	if s.HTTPAddress() != "" {
		t.Error("still listening")
	}
}

// With TLS the listener serves HTTPS; a caller that trusts the certificate reaches it
// (S40a).
func TestHTTPListenerTLS(t *testing.T) {
	_, s := httpSetup(t)
	dir := t.TempDir()
	c, _, err := tlscert.SelfSigned(dir, []string{"localhost"}, []net.IP{net.IPv4(127, 0, 0, 1)}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s.opts.Listen, s.opts.TLS = "127.0.0.1:0", tlscert.Static(c)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	var addr string
	for deadline := time.Now().Add(5 * time.Second); addr == ""; time.Sleep(10 * time.Millisecond) {
		if addr = s.HTTPAddress(); addr == "" && time.Now().After(deadline) {
			t.Fatal("not listening")
		}
	}
	pool := x509.NewCertPool()
	pem, _ := os.ReadFile(filepath.Join(dir, tlscert.CertFile))
	pool.AppendCertsFromPEM(pem)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	resp, err := client.Get("https://" + addr + "/open?ID=3")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("https: %d", resp.StatusCode)
	}
	// Plain HTTP and callers that do not trust the certificate get no answer.
	if resp, err := http.Get("http://" + addr + "/open?ID=3"); err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("plain HTTP: %d", resp.StatusCode)
		}
	}
	if _, err := http.Get("https://" + addr + "/open?ID=3"); err == nil {
		t.Error("untrusted certificate accepted")
	}
}

// On the network, the Host header is the name callers use; requests from web pages
// are still refused, and auth: none and plain HTTP are warned about.
func TestHTTPOnNetwork(t *testing.T) {
	f, s := httpSetup(t)
	s.opts.Listen = "0.0.0.0:0"
	s.Reload()
	if w := (call{method: "GET", path: "/open?ID=1", host: "srv01.example:7301"}).do(s); w.Code != 202 {
		t.Errorf("host on the network: %d %s", w.Code, w.Body)
	}
	if w := (call{method: "GET", path: "/open?ID=1", origin: "https://evil.example"}).do(s); w.Code != 403 {
		t.Errorf("origin: %d", w.Code)
	}
	s.http.listen()
	defer s.http.close()
	logs := strings.Join(f.logs, "\n")
	if !strings.Contains(logs, `trigger "open": auth: none on the network; anyone who reaches 0.0.0.0:0 can start runs`) ||
		strings.Contains(logs, `trigger "items": auth: none`) ||
		!strings.Contains(logs, "http triggers: listening on the network without TLS (--triggers-insecure)") {
		t.Errorf("logs %v", f.logs)
	}
}

// With response: run the caller gets the first answer of the run, the default answer
// when the run ends, or 504 after the timeout (S38b1).
func TestHTTPResponseRun(t *testing.T) {
	t.Setenv("RUNNEL_TEST_TOKEN", "s3cret")
	dir, f, s, _ := setup(t, map[string]string{
		"svc.yaml": httpTrigger("svc", "POST", "/svc/{ID}", "") + "  auth: { token: \"${env:RUNNEL_TEST_TOKEN}\" }\n  response: run\n  timeout: 1s\n  maxConcurrent: 0\n",
	})
	write(t, dir, "workflows/api.workflow.yaml", apiTarget)
	s.Reload()
	var behave func(id string, r StartRequest) // what the simulated run does
	outcomes := map[string]Outcome{}
	s.opts.Start = func(tr *model.Trigger, r StartRequest) (Started, error) {
		run, err := f.start(tr, r)
		go behave(run.ID, r)
		run.Wait = func(ctx context.Context) (Outcome, bool) {
			f.mu.Lock()
			out, ok := outcomes[run.ID]
			f.mu.Unlock()
			if !ok {
				<-ctx.Done()
				return Outcome{Status: "running"}, true
			}
			return out, true
		}
		return run, err
	}
	post := func(body string) *httptest.ResponseRecorder {
		return call{method: "POST", path: "/svc/7?q=1", token: "s3cret", contentType: "application/json", body: body}.do(s)
	}

	// The first http.response answers; a second one is refused.
	second := make(chan bool, 1)
	behave = func(_ string, r StartRequest) {
		r.Respond(action.Response{Status: 201, Headers: map[string]string{"X-Item": "7"}, Body: map[string]any{"created": true}})
		second <- r.Respond(action.Response{Status: 500})
	}
	w := post(`{"NAME": "a", "DATA": {"n": 3, "x": 1.5}}`)
	if w.Code != 201 || w.Header().Get("X-Item") != "7" || w.Header().Get("Runnel-Run-Id") != "run-1" ||
		w.Header().Get("Content-Type") != "application/json; charset=utf-8" || strings.TrimSpace(w.Body.String()) != `{"created":true}` {
		t.Errorf("respond: %d %v %s", w.Code, w.Header(), w.Body)
	}
	if <-second {
		t.Error("second answer accepted")
	}
	req := f.requests[0].Request
	body, _ := req["body"].(map[string]any)
	headers, _ := req["headers"].(map[string]any)
	if req["method"] != "POST" || req["path"] != "/svc/7" || req["query"].(map[string]any)["q"] != "1" || req["client"] != "192.0.2.7" ||
		body["NAME"] != "a" || body["DATA"].(map[string]any)["n"] != int64(3) || body["DATA"].(map[string]any)["x"] != 1.5 ||
		headers["Content-Type"] != "application/json" || headers["Authorization"] != nil {
		t.Errorf("request %v", req)
	}

	// A text answer is sent as text.
	behave = func(_ string, r StartRequest) { r.Respond(action.Response{Status: 422, Body: "missing name"}) }
	if w := post(""); w.Code != 422 || w.Body.String() != "missing name" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("text: %d %v %q", w.Code, w.Header(), w.Body)
	}

	// Without http.response: the outputs when the run has succeeded, 500 when it failed.
	behave = func(string, StartRequest) {}
	f.mu.Lock()
	outcomes["run-3"] = Outcome{Status: "succeeded", Body: map[string]any{"MESSAGE": "hi"}}
	outcomes["run-4"] = Outcome{Status: "failed", Error: "disk full"}
	f.mu.Unlock()
	if w := post(""); w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"MESSAGE":"hi"}` {
		t.Errorf("default: %d %s", w.Code, w.Body)
	}
	if w := post(""); w.Code != 500 || !strings.Contains(w.Body.String(), `"error":"the run failed; see run run-4"`) || strings.Contains(w.Body.String(), "disk full") {
		t.Errorf("failed: %d %s", w.Code, w.Body)
	}
	if !strings.Contains(strings.Join(f.logs, "\n"), `trigger "svc": run run-4 failed without an answer: disk full`) {
		t.Errorf("logs %v", f.logs)
	}

	// No answer within the timeout: 504; the run goes on.
	if w := post(""); w.Code != 504 || !strings.Contains(w.Body.String(), "no answer within 1s; the run run-5 goes on") {
		t.Errorf("timeout: %d %s", w.Code, w.Body)
	}
}

// For a pipeline the body may be a JSON list of objects: the records for
// core.from-result (S38c). An answer with whenEmpty is sent only if no other comes.
func TestHTTPPipeline(t *testing.T) {
	dir, f, s, _ := setup(t, map[string]string{
		"load.yaml": "kind: Trigger\napiVersion: runnel/v1\nname: load\ntype: http\ntarget: pipelines/load.pipeline.yaml\n" +
			"with:\n  method: POST\n  path: /load\n  auth: none\n  response: run\n  timeout: 1s\n",
	})
	write(t, dir, "pipelines/load.pipeline.yaml", "kind: Pipeline\napiVersion: runnel/v1\nname: load\nparameters:\n  - { name: MODE }\n"+
		"transforms:\n  - { id: in, type: core.from-result }\n  - { id: out, type: core.dummy }\nlinks:\n  - { from: in, to: out }\n")
	s.Reload()
	if p := s.Problems(); len(p) != 0 {
		t.Fatal(p)
	}
	var behave func(r StartRequest)
	s.opts.Start = func(tr *model.Trigger, r StartRequest) (Started, error) {
		run, err := f.start(tr, r)
		behave(r)
		run.Wait = func(context.Context) (Outcome, bool) {
			return Outcome{Status: "succeeded", Body: map[string]any{"transforms": map[string]any{}}}, true
		}
		return run, err
	}
	post := func(body string) *httptest.ResponseRecorder {
		return call{method: "POST", path: "/load?MODE=full", contentType: "application/json", body: body}.do(s)
	}

	// The fallback is the answer when no other comes; another answer wins over it.
	behave = func(r StartRequest) {
		r.Respond(action.Response{Status: 404, Body: json.RawMessage("null"), Fallback: true})
	}
	if w := post(`[{"id": 1, "name": "a"}, {"id": 2.5}]`); w.Code != 404 || strings.TrimSpace(w.Body.String()) != "null" {
		t.Errorf("fallback: %d %s", w.Code, w.Body)
	}
	if rows := f.requests[0].Rows; len(rows) != 2 || rows[0]["id"] != int64(1) || rows[1]["id"] != 2.5 || f.params[0]["MODE"] != "full" {
		t.Errorf("rows %v, params %v", rows, f.params[0])
	}
	behave = func(r StartRequest) {
		r.Respond(action.Response{Status: 404, Fallback: true})
		r.Respond(action.Response{Status: 200, Body: json.RawMessage(`[{"id":1}]`)})
	}
	if w := post(`[]`); w.Code != 200 || strings.TrimSpace(w.Body.String()) != `[{"id":1}]` || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Errorf("answer over fallback: %d %s", w.Code, w.Body)
	}
	behave = func(StartRequest) {}
	if w := post(`[1, 2]`); w.Code != 400 || !strings.Contains(w.Body.String(), "entry 1 of the JSON list is not an object") {
		t.Errorf("list of numbers: %d %s", w.Code, w.Body)
	}
	if w := post(`"text"`); w.Code != 400 || !strings.Contains(w.Body.String(), "a JSON object (parameters) or a list of objects (records)") {
		t.Errorf("text: %d %s", w.Code, w.Body)
	}
	// Without an answer, the counts of the transforms (default answer).
	if w := post(`{"MODE": "delta"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"transforms"`) || f.params[len(f.params)-1]["MODE"] != "full" {
		t.Errorf("default: %d %s %v", w.Code, w.Body, f.params)
	}
}

// For a workflow a JSON list is not accepted.
func TestHTTPWorkflowList(t *testing.T) {
	_, s := httpSetup(t)
	if w := (call{method: "GET", path: "/open?ID=1", contentType: "application/json", body: `[{"a": 1}]`}).do(s); w.Code != 400 ||
		!strings.Contains(w.Body.String(), "the body must be a JSON object") {
		t.Errorf("%d %s", w.Code, w.Body)
	}
}

// The JSON body of http.response belongs to the result of the run: writing the answer adds
// the line break to a copy, never into spare room of the caller's slice.
func TestWriteResponseKeepsBody(t *testing.T) {
	buf := make([]byte, 0, 32)
	buf = append(buf, `{"ok":true}`...)
	spare := buf[:cap(buf)]
	spare[len(buf)] = 'x'
	rec := httptest.NewRecorder()
	writeResponse(rec, action.Response{Status: http.StatusOK, Body: json.RawMessage(buf)})
	if got := rec.Body.String(); got != "{\"ok\":true}\n" {
		t.Errorf("body = %q", got)
	}
	if spare[len(buf)] != 'x' {
		t.Error("writeResponse wrote into the body's backing array")
	}
}
