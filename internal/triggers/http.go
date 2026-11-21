package triggers

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/runs"
)

// MaxBody is the largest request body an http trigger accepts (413 above).
const MaxBody = 1 << 20

// DefaultListen is the address of the listener of http triggers unless given.
const DefaultListen = "127.0.0.1:7301"

// listener serves the http triggers on their own address, apart from the editor.
type listener struct {
	mu      sync.Mutex
	srv     *http.Server
	addr    string
	problem string // last error opening it (logged once)
}

// listen opens the listener once an http trigger is enabled; a failure is logged once
// and tried again at the next check.
func (s *Scheduler) listen() {
	if s.opts.Listen == "" {
		return
	}
	s.mu.Lock()
	want := slices.ContainsFunc(s.entries, func(e *entry) bool { return e.t.Type == model.TriggerHTTP && e.t.IsEnabled() })
	s.mu.Unlock()
	l := &s.listener
	l.mu.Lock()
	defer l.mu.Unlock()
	if !want || l.srv != nil {
		return
	}
	ln, err := net.Listen("tcp", s.opts.Listen)
	if err != nil {
		msg := fmt.Sprintf("http triggers: cannot listen on %s (is the port taken? use --triggers): %v", s.opts.Listen, err)
		if msg != l.problem {
			l.problem = msg
			s.opts.Warn(msg)
		}
		return
	}
	l.problem = ""
	l.addr = ln.Addr().String()
	l.srv = &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: time.Minute}
	go func(srv *http.Server) { _ = srv.Serve(ln) }(l.srv)
	s.opts.Log(fmt.Sprintf("http triggers: listening on http://%s", l.addr))
}

func (l *listener) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = l.srv.Shutdown(ctx)
		l.srv = nil
	}
}

// HTTPAddress returns the address the http triggers listen on; empty if not (yet).
func (s *Scheduler) HTTPAddress() string {
	s.listener.mu.Lock()
	defer s.listener.mu.Unlock()
	if s.listener.srv == nil {
		return ""
	}
	return s.listener.addr
}

type errorBody struct {
	Error string `json:"error"`
}

type acceptedBody struct {
	RunID  string `json:"runId"`
	Status string `json:"status"`
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, format string, args ...any) {
	reply(w, status, errorBody{fmt.Sprintf(format, args...)})
}

// ServeHTTP answers the requests to http triggers (S38a, ADR 0018): it finds the
// trigger by method and path, checks the token, turns the path, the query and the body
// into parameters of the target and starts a run (202 with its id).
func (s *Scheduler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Until the listener can be reached from the network (S40a), only programs on this
	// computer are callers: web pages in a browser must not start runs (cross-site
	// requests send an Origin header; DNS rebinding a foreign Host).
	if !isLoopbackHost(r.Host) {
		fail(w, http.StatusForbidden, "requests must be addressed to localhost or 127.0.0.1")
		return
	}
	if r.Header.Get("Origin") != "" {
		fail(w, http.StatusForbidden, "requests from web pages (with an Origin header) are not accepted")
		return
	}
	e, values, allowed := s.route(r.Method, r.URL.Path)
	switch {
	case e == nil && len(allowed) > 0:
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		fail(w, http.StatusMethodNotAllowed, "method %s is not allowed for %s (allowed: %s)", r.Method, r.URL.Path, strings.Join(allowed, ", "))
		return
	case e == nil:
		fail(w, http.StatusNotFound, "no trigger for %s %s", r.Method, r.URL.Path)
		return
	case !e.t.IsEnabled():
		fail(w, http.StatusNotFound, "trigger %q is disabled", e.t.Name)
		return
	}
	t := e.t
	if !t.AuthNone() && !authorized(t, r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="runnel"`)
		fail(w, http.StatusUnauthorized, "missing or wrong token (expected the header Authorization: Bearer <token>)")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
	if err != nil {
		if errors.As(err, new(*http.MaxBytesError)) {
			fail(w, http.StatusRequestEntityTooLarge, "the body is larger than %d bytes", MaxBody)
		} else {
			fail(w, http.StatusBadRequest, "cannot read the body: %v", err)
		}
		return
	}
	inputs, status, err := requestValues(r, body)
	if err != nil {
		fail(w, status, "%v", err)
		return
	}
	for k, v := range values { // path before query before body
		inputs[k] = v
	}
	params, err := s.targetParams(t, inputs)
	var bad *badRequest
	switch {
	case errors.As(err, &bad):
		fail(w, http.StatusBadRequest, "%s", bad.msg)
		return
	case err != nil:
		s.opts.Warn(fmt.Sprintf("trigger %q: %v", t.Name, err))
		fail(w, http.StatusInternalServerError, "the trigger cannot start its target; see the server log")
		return
	}
	client := r.RemoteAddr
	if host, _, err := net.SplitHostPort(client); err == nil {
		client = host
	}
	s.startHTTP(w, e, params, client)
}

// isLoopbackHost reports whether a Host header names the loopback interface.
func isLoopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// route finds the http trigger for a method and path; with a matching path but
// another method it returns the allowed methods.
func (s *Scheduler) route(method, path string) (*entry, map[string]string, []string) {
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
	want, ok := t.Token()
	if !ok {
		return false
	}
	got, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return found && subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(want)) == 1
}

// requestValues returns the values of the query and the body (a JSON object or a
// form); the query wins over the body. Non-text JSON values are given as JSON.
func requestValues(r *http.Request, body []byte) (map[string]string, int, error) {
	values := map[string]string{}
	if len(bytes.TrimSpace(body)) > 0 {
		media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		switch {
		case media == "application/x-www-form-urlencoded":
			form, err := url.ParseQuery(string(body))
			if err != nil {
				return nil, http.StatusBadRequest, fmt.Errorf("cannot read the form in the body: %w", err)
			}
			for k, v := range form {
				values[k] = v[0]
			}
		case media == "" || media == "application/json" || strings.HasSuffix(media, "+json"):
			dec := json.NewDecoder(bytes.NewReader(body))
			dec.UseNumber()
			var obj map[string]any
			if err := dec.Decode(&obj); err != nil || obj == nil {
				return nil, http.StatusBadRequest, errors.New("the body must be a JSON object, e.g. {\"id\": 4711}")
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
			return nil, http.StatusUnsupportedMediaType, fmt.Errorf("unsupported content type %q (expected application/json or application/x-www-form-urlencoded)", media)
		}
	}
	for k, v := range r.URL.Query() {
		values[k] = v[0]
	}
	return values, 0, nil
}

type badRequest struct{ msg string }

func (b *badRequest) Error() string { return b.msg }

// targetParams works out the parameters of a run: the fixed params of the trigger,
// then the values of the request for the other declared parameters (others are
// ignored). A wrong or missing value is a badRequest.
func (s *Scheduler) targetParams(t *model.Trigger, inputs map[string]string) (map[string]string, error) {
	declared, what, err := runs.TargetParams(t, s.opts.Dir)
	if err != nil {
		return nil, err
	}
	params := runs.TriggerParams(t)
	if params == nil {
		params = map[string]string{}
	}
	var problems, missing []string
	for _, p := range declared {
		if _, fixed := params[p.Name]; fixed {
			continue
		}
		v, given := inputs[p.Name]
		switch {
		case given:
			if _, err := p.CheckValue(v); err != nil {
				problems = append(problems, fmt.Sprintf("parameter %s (%s): %v", p.Name, p.Type.OrString(), err))
				continue
			}
			params[p.Name] = v
		case p.Required && p.Default == "":
			missing = append(missing, p.Name)
		}
	}
	if len(missing) > 0 {
		problems = append([]string{fmt.Sprintf("missing required parameter %s of %s", strings.Join(missing, ", "), what)}, problems...)
	}
	if len(problems) > 0 {
		return nil, &badRequest{strings.Join(problems, "; ")}
	}
	return params, nil
}

// startHTTP starts the run of a request unless maxConcurrent or a limit of the
// project is reached.
func (s *Scheduler) startHTTP(w http.ResponseWriter, e *entry, params map[string]string, client string) {
	t := e.t
	e.startMu.Lock()
	defer e.startMu.Unlock()
	s.mu.Lock()
	e.active = slices.DeleteFunc(e.active, func(id string) bool { return !s.opts.Running(id) })
	running := len(e.active)
	s.mu.Unlock()
	if limit := t.With.MaxConcurrent; limit > 0 && running >= limit {
		runsText := fmt.Sprintf("%d runs", running)
		if running == 1 {
			runsText = "1 run"
		}
		fail(w, http.StatusTooManyRequests, "trigger %q is already running %s (maxConcurrent: %d); try again later", t.Name, runsText, limit)
		return
	}
	now := s.opts.Now()
	id, err := s.opts.Start(t, params, client)
	switch {
	case errors.As(err, new(*runs.LimitError)):
		fail(w, http.StatusTooManyRequests, "%v", err)
		return
	case errors.Is(err, runs.ErrShutdown):
		fail(w, http.StatusServiceUnavailable, "%v", err)
		return
	case err != nil:
		s.opts.Warn(fmt.Sprintf("trigger %q: %s was not started: %v", t.Name, t.Target, err))
		s.mu.Lock()
		e.last = &Fired{Time: now, Error: err.Error()}
		s.mu.Unlock()
		fail(w, http.StatusInternalServerError, "the run could not be started; see the server log")
		return
	}
	s.mu.Lock()
	e.active = append(e.active, id)
	e.last = &Fired{Time: now, RunID: id}
	s.mu.Unlock()
	s.opts.Log(fmt.Sprintf("trigger %q: started %s for %s, run %s", t.Name, t.Target, client, id))
	w.Header().Set("Location", "/api/v1/runs/"+id)
	reply(w, http.StatusAccepted, acceptedBody{RunID: id, Status: "/api/v1/runs/" + id})
}
