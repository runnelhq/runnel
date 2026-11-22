package triggers

import (
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"mime"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/runnelhq/runnel/internal/httpx"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/runs"
)

// MaxBody is the largest request body an http trigger accepts (413 above).
const MaxBody = 1 << 20

// DefaultListen is the address of the listener of http triggers unless given.
const DefaultListen = "127.0.0.1:7301"

// httpTriggers serves the http triggers on their own address, apart from the editor
// (S38a, ADR 0018): the listener, finding the trigger of a request, its parameters and
// starting its run.
type httpTriggers struct {
	s *Scheduler

	mu      sync.Mutex
	srv     *http.Server
	addr    string
	problem string // last error opening it (logged once)
}

// listen opens the listener once an http trigger is enabled; a failure is logged once
// and tried again at the next check.
func (h *httpTriggers) listen() {
	s := h.s
	if s.opts.Listen == "" {
		return
	}
	s.mu.Lock()
	want := slices.ContainsFunc(s.entries, func(e *entry) bool { return e.t.Type == model.TriggerHTTP && e.t.IsEnabled() })
	s.mu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	if !want || h.srv != nil {
		return
	}
	ln, err := new(net.ListenConfig).Listen(context.Background(), "tcp", s.opts.Listen)
	if err != nil {
		msg := fmt.Sprintf("http triggers: cannot listen on %s (is the port taken? use --triggers): %v", s.opts.Listen, err)
		if msg != h.problem {
			h.problem = msg
			s.opts.Warn(msg)
		}
		return
	}
	h.problem = ""
	h.addr = ln.Addr().String()
	scheme := "http"
	if s.opts.TLS != nil {
		ln, scheme = tls.NewListener(ln, s.opts.TLS), "https"
	}
	h.srv = &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: time.Minute, TLSConfig: s.opts.TLS,
		ErrorLog: log.New(io.Discard, "", 0)} // failed TLS handshakes of scanners are no news
	go func(srv *http.Server) { _ = srv.Serve(ln) }(h.srv)
	s.opts.Log(fmt.Sprintf("http triggers: listening on %s://%s", scheme, h.addr))
	if !s.local() && s.opts.TLS == nil {
		s.opts.Warn("http triggers: listening on the network without TLS (--triggers-insecure); tokens and data are sent as plain text")
	}
}

// local reports whether the listener of http triggers is reachable from this computer
// only (a loopback address).
func (s *Scheduler) local() bool {
	host, _, err := net.SplitHostPort(s.opts.Listen)
	if err != nil {
		return true
	}
	return httpx.IsLoopback(host)
}

func (h *httpTriggers) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = h.srv.Shutdown(ctx)
		h.srv = nil
	}
}

// HTTPAddress returns the address the http triggers listen on; empty if not (yet).
func (s *Scheduler) HTTPAddress() string {
	h := &s.http
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.srv == nil {
		return ""
	}
	return h.addr
}

type acceptedBody struct {
	RunID  string `json:"runId"`
	Status string `json:"status"`
}

func reply(w http.ResponseWriter, status int, v any) { httpx.WriteJSON(w, status, v, false) }

// httpError is the answer to a request whose run is not started: a status and a
// message for the caller, without internal details (those go to the server log).
type httpError struct {
	status int
	msg    string
	header [2]string // a header to set, e.g. Allow
}

func (e *httpError) Error() string { return e.msg }

func errorf(status int, format string, args ...any) *httpError {
	return &httpError{status: status, msg: fmt.Sprintf(format, args...)}
}

func (e *httpError) write(w http.ResponseWriter) {
	if e.header[0] != "" {
		w.Header().Set(e.header[0], e.header[1])
	}
	reply(w, e.status, httpx.ErrorBody{Error: e.msg})
}

// ServeHTTP answers the requests to http triggers.
func (s *Scheduler) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.http.ServeHTTP(w, r) }

// ServeHTTP finds the trigger by method and path, checks the token, turns the path,
// the query and the body into parameters of the target and starts a run: 202 with its
// id, or the answer of the run (response: run, respond.go).
func (h *httpTriggers) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e, start, herr := h.request(w, r)
	if herr != nil {
		herr.write(w)
		return
	}
	if e.t.WaitsForRun() {
		h.answer(w, r, e, start)
		return
	}
	id, herr := h.start(e, start)
	if herr != nil {
		herr.write(w)
		return
	}
	w.Header().Set("Location", "/api/v1/runs/"+id)
	reply(w, http.StatusAccepted, acceptedBody{RunID: id, Status: "/api/v1/runs/" + id})
}

// request checks a request and works out the start of its run.
func (h *httpTriggers) request(w http.ResponseWriter, r *http.Request) (*entry, StartRequest, *httpError) {
	// Web pages in a browser must not start runs: cross-site requests send an Origin
	// header; on a local listener, DNS rebinding sends a foreign Host.
	if h.s.local() && !httpx.IsLoopback(r.Host) {
		return nil, StartRequest{}, errorf(http.StatusForbidden, "requests must be addressed to localhost or 127.0.0.1")
	}
	if r.Header.Get("Origin") != "" {
		return nil, StartRequest{}, errorf(http.StatusForbidden, "requests from web pages (with an Origin header) are not accepted")
	}
	e, values, allowed := h.route(r.Method, r.URL.Path)
	switch {
	case e == nil && len(allowed) > 0:
		methods := strings.Join(allowed, ", ")
		herr := errorf(http.StatusMethodNotAllowed, "method %s is not allowed for %s (allowed: %s)", r.Method, r.URL.Path, methods)
		herr.header = [2]string{"Allow", methods}
		return nil, StartRequest{}, herr
	case e == nil:
		return nil, StartRequest{}, errorf(http.StatusNotFound, "no trigger for %s %s", r.Method, r.URL.Path)
	case !e.t.IsEnabled():
		return nil, StartRequest{}, errorf(http.StatusNotFound, "trigger %q is disabled", e.t.Name)
	}
	t := e.t
	if !t.AuthNone() && !authorized(t, r) {
		herr := errorf(http.StatusUnauthorized, "missing or wrong token (expected the header Authorization: Bearer <token>)")
		herr.header = [2]string{"WWW-Authenticate", `Bearer realm="runnel"`}
		return nil, StartRequest{}, herr
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
	if err != nil {
		if errors.As(err, new(*http.MaxBytesError)) {
			return nil, StartRequest{}, errorf(http.StatusRequestEntityTooLarge, "the body is larger than %d bytes", MaxBody)
		}
		return nil, StartRequest{}, errorf(http.StatusBadRequest, "cannot read the body: %v", err)
	}
	inputs, bodyValue, herr := requestValues(r, body, strings.HasSuffix(t.Target, ".pipeline.yaml"))
	if herr != nil {
		return nil, StartRequest{}, herr
	}
	// path before query before body
	maps.Copy(inputs, values)
	params, herr := h.params(t, inputs)
	if herr != nil {
		return nil, StartRequest{}, herr
	}
	client := r.RemoteAddr
	if host, _, err := net.SplitHostPort(client); err == nil {
		client = host
	}
	start := StartRequest{Params: params, Client: client, Request: requestOf(r, bodyValue, client), History: t.HistoryMode()}
	if list, ok := bodyValue.([]any); ok {
		for _, item := range list {
			start.Rows = append(start.Rows, item.(map[string]any))
		}
	}
	return e, start, nil
}

// route finds the http trigger for a method and path; with a matching path but
// another method it returns the allowed methods.
func (h *httpTriggers) route(method, path string) (*entry, map[string]string, []string) {
	s := h.s
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		parts = nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var allowed []string
	for _, e := range s.entries {
		if e.t.Type != model.TriggerHTTP {
			continue
		}
		values, ok := match(e.t.Segments(), parts)
		if !ok {
			continue
		}
		if e.t.HTTPMethod() == method {
			return e, values, nil
		}
		if e.t.IsEnabled() && !slices.Contains(allowed, e.t.HTTPMethod()) {
			allowed = append(allowed, e.t.HTTPMethod())
		}
	}
	slices.Sort(allowed)
	return nil, nil, allowed
}

// match compares the parts of a request path with the segments of a route; fixed
// parts ignore case.
func match(segments []model.Segment, parts []string) (map[string]string, bool) {
	if len(segments) != len(parts) {
		return nil, false
	}
	values := map[string]string{}
	for i, seg := range segments {
		switch {
		case seg.Placeholder:
			values[seg.Text] = parts[i]
		case !strings.EqualFold(seg.Text, parts[i]):
			return nil, false
		}
	}
	return values, true
}

func authorized(t *model.Trigger, r *http.Request) bool {
	want, err := t.Token()
	if err != nil {
		return false
	}
	got, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return found && subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(want)) == 1
}

// requestValues returns the values of the query and the body (a JSON object or a
// form) as text, and the body as a value for request.body; the query wins over the
// body. Non-text JSON values are given as JSON. With list (a pipeline is the target),
// the body may also be a JSON list of objects: the records for core.from-result.
func requestValues(r *http.Request, body []byte, list bool) (map[string]string, any, *httpError) {
	values := map[string]string{}
	var bodyValue any
	if len(bytes.TrimSpace(body)) > 0 {
		media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		switch {
		case media == "application/x-www-form-urlencoded":
			form, err := url.ParseQuery(string(body))
			if err != nil {
				return nil, nil, errorf(http.StatusBadRequest, "cannot read the form in the body: %v", err)
			}
			fields := map[string]any{}
			for k, v := range form {
				values[k], fields[k] = v[0], v[0]
			}
			bodyValue = fields
		case media == "" || media == "application/json" || strings.HasSuffix(media, "+json"):
			dec := json.NewDecoder(bytes.NewReader(body))
			dec.UseNumber()
			var v any
			err := dec.Decode(&v)
			obj, isObject := v.(map[string]any)
			items, isList := v.([]any)
			switch {
			case err == nil && isObject:
				bodyValue = httpx.Numbers(obj)
			case err == nil && isList && list:
				for i, item := range items {
					if _, ok := item.(map[string]any); !ok {
						return nil, nil, errorf(http.StatusBadRequest, "entry %d of the JSON list is not an object; the list holds the records for the pipeline", i+1)
					}
				}
				return queryValues(r, values), httpx.Numbers(items), nil
			case list:
				return nil, nil, errorf(http.StatusBadRequest, "%s", "the body must be a JSON object (parameters) or a list of objects (records), e.g. [{\"id\": 4711}]")
			default:
				return nil, nil, errorf(http.StatusBadRequest, "%s", "the body must be a JSON object, e.g. {\"id\": 4711}")
			}
			for k, v := range obj {
				switch v := v.(type) {
				case nil:
				case string:
					values[k] = v
				case json.Number:
					values[k] = v.String()
				case bool:
					values[k] = fmt.Sprint(v)
				default:
					data, _ := json.Marshal(v)
					values[k] = string(data)
				}
			}
		default:
			return nil, nil, errorf(http.StatusUnsupportedMediaType, "unsupported content type %q (expected application/json or application/x-www-form-urlencoded)", media)
		}
	}
	return queryValues(r, values), bodyValue, nil
}

// queryValues adds the query parameters to values; they win over the body.
func queryValues(r *http.Request, values map[string]string) map[string]string {
	for k, v := range r.URL.Query() {
		values[k] = v[0]
	}
	return values
}

// params works out the parameters of a run: the fixed params of the trigger, then the
// values of the request for the other declared parameters (others are ignored). A
// wrong or missing value is 400; a target that cannot be read 500 (logged).
func (h *httpTriggers) params(t *model.Trigger, inputs map[string]string) (map[string]string, *httpError) {
	declared, what, err := TargetParams(t, h.s.opts.Dir)
	if err != nil {
		h.s.opts.Warn(fmt.Sprintf("trigger %q: %v", t.Name, err))
		return nil, errorf(http.StatusInternalServerError, "the trigger cannot start its target; see the server log")
	}
	params := TriggerParams(t)
	if params == nil {
		params = map[string]string{}
	}
	fixed := func(name string) bool { _, ok := params[name]; return ok }
	problems, missing := runs.CheckValues(declared, inputs, fixed)
	for _, p := range declared {
		if v, given := inputs[p.Name]; given && !fixed(p.Name) {
			params[p.Name] = v
		}
	}
	if len(missing) > 0 {
		problems = append([]string{fmt.Sprintf("missing required parameter %s of %s", strings.Join(missing, ", "), what)}, problems...)
	}
	if len(problems) > 0 {
		return nil, errorf(http.StatusBadRequest, "%s", strings.Join(problems, "; "))
	}
	return params, nil
}

// start starts the run of a request unless maxConcurrent or a limit of the project is
// reached.
func (h *httpTriggers) start(e *entry, start StartRequest) (string, *httpError) {
	s, t := h.s, e.t
	if t.With.MaxConcurrent > 0 {
		// Counting the running runs and starting one must not interleave; without a
		// limit, requests start their runs at the same time.
		e.startMu.Lock()
		defer e.startMu.Unlock()
	}
	s.dropEnded(e)
	s.mu.Lock()
	running := len(e.active)
	s.mu.Unlock()
	if limit := t.With.MaxConcurrent; limit > 0 && running >= limit {
		runsText := fmt.Sprintf("%d runs", running)
		if running == 1 {
			runsText = "1 run"
		}
		return "", errorf(http.StatusTooManyRequests, "trigger %q is already running %s (maxConcurrent: %d); try again later", t.Name, runsText, limit)
	}
	now := s.opts.Now()
	id, err := s.opts.Start(t, start)
	switch {
	case errors.As(err, new(*runs.LimitError)):
		return "", errorf(http.StatusTooManyRequests, "%v", err)
	case errors.Is(err, runs.ErrShutdown):
		return "", errorf(http.StatusServiceUnavailable, "%v", err)
	case err != nil:
		s.opts.Warn(fmt.Sprintf("trigger %q: %s was not started: %v", t.Name, t.Target, err))
		s.mu.Lock()
		e.last = &Fired{Time: now, Error: err.Error()}
		s.mu.Unlock()
		return "", errorf(http.StatusInternalServerError, "the run could not be started; see the server log")
	}
	s.mu.Lock()
	e.active = append(e.active, id)
	e.last = &Fired{Time: now, RunID: id}
	s.mu.Unlock()
	s.opts.Log(fmt.Sprintf("trigger %q: started %s for %s, run %s", t.Name, t.Target, start.Client, id))
	return id, nil
}
