package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/scaffold"
	"github.com/runnelhq/runnel/internal/version"
	"github.com/runnelhq/runnel/internal/webui"
)

// Error codes of the responses about the project of the folder (R10).
const (
	CodeNoProject     = "no_project"
	CodeProjectExists = "project_exists"
	CodeNotEmpty      = "not_empty"
	CodeInvalidName   = "invalid_name"
)

// Host serves the folder of "runnel serve" (R10): the project in it, or – while the
// folder has no project – the editor with its start page, version, health and
// /project, which creates the project like "runnel init". Then the host opens the
// project like at the start and serves it, without a restart; a project.yaml created
// otherwise (e.g. "runnel init" in a terminal) is picked up with the next request.
type Host struct {
	opts    Options
	started time.Time
	empty   http.Handler
	// handler serves the project once it is open; nil until then.
	handler atomic.Pointer[http.Handler]
	// mu serializes opening the project and stopping.
	mu      sync.Mutex
	srv     *Server
	ctx     context.Context // of StartTriggers; nil before
	stopped bool
}

// Open returns the host of the folder opts.Dir; the project in it is opened at once
// if there is one.
func Open(opts Options) (*Host, error) {
	if opts.UI == nil {
		opts.UI = webui.FS()
	}
	h := &Host{opts: opts, started: time.Now()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+APIPrefix+"/version", handleVersion)
	mux.HandleFunc("GET "+APIPrefix+"/health", h.handleHealth)
	mux.HandleFunc(APIPrefix+"/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusConflict, apiError{Error: "there is no project in this folder yet; create one first", Code: CodeNoProject})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { serveUI(opts.UI, w, r) })
	h.empty = mux
	if h.hasProjectFile() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if err := h.open(); err != nil {
			return nil, err
		}
	}
	return h, nil
}

// HasProject reports whether the host serves a project.
func (h *Host) HasProject() bool { return h.handler.Load() != nil }

func (h *Host) hasProjectFile() bool {
	info, err := os.Stat(filepath.Join(h.opts.Dir, model.ProjectFile))
	return err == nil && !info.IsDir()
}

// open opens the project and serves it; h.mu must be held.
func (h *Host) open() error {
	srv, err := New(h.opts)
	if err != nil {
		return err
	}
	srv.started = h.started // GET /health counts from the start of the process
	h.srv = srv
	handler := srv.Handler()
	h.handler.Store(&handler)
	if h.ctx != nil {
		srv.StartTriggers(h.ctx)
	}
	return nil
}

// current returns the handler of the project, opening it if its project.yaml has
// appeared; nil while there is none.
func (h *Host) current() http.Handler {
	if p := h.handler.Load(); p != nil {
		return *p
	}
	if !h.hasProjectFile() {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.srv == nil && !h.stopped {
		if err := h.open(); err != nil {
			h.warnf("cannot open the project in %s: %v", h.opts.Dir, err)
			return nil
		}
		h.logf("found project in %s; serving it now", h.opts.Dir)
	}
	if p := h.handler.Load(); p != nil {
		return *p
	}
	return nil
}

// Handler returns the HTTP handler: GET and POST /project, everything else from the
// project or, without one, from the start page.
func (h *Host) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+APIPrefix+"/project", h.handleGetProject)
	mux.HandleFunc("POST "+APIPrefix+"/project", h.handleCreateProject)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if p := h.current(); p != nil {
			p.ServeHTTP(w, r)
			return
		}
		h.empty.ServeHTTP(w, r)
	})
	return guard(mux)
}

// ProjectStatus is the body of GET /project.
type ProjectStatus struct {
	Exists bool   `json:"exists"`
	Dir    string `json:"dir"`
	// Name is the name of the project; without one, DefaultName is the name a new
	// project gets (from the folder) and Empty tells whether the folder has no files yet.
	Name        string `json:"name,omitempty"`
	DefaultName string `json:"defaultName,omitempty"`
	Empty       bool   `json:"empty"`
}

func (h *Host) handleGetProject(w http.ResponseWriter, _ *http.Request) {
	st := ProjectStatus{Dir: h.opts.Dir}
	if h.current() != nil {
		st.Exists = true
		if p, err := model.LoadProject(h.opts.Dir); err == nil {
			st.Name = p.Name
		}
	} else {
		st.DefaultName = scaffold.DefaultName(h.opts.Dir)
		st.Empty, _ = scaffold.IsEmpty(h.opts.Dir)
	}
	writeJSON(w, http.StatusOK, st)
}

// createRequest is the body of POST /project; the folder is the server's, never one
// of the browser.
type createRequest struct {
	Name string `json:"name"`
	// Force adds the files to a folder that is not empty, like "runnel init --force";
	// existing files are kept.
	Force bool `json:"force"`
}

type createResponse struct {
	Name    string   `json:"name"`
	Dir     string   `json:"dir"`
	Created []string `json:"created"`
	Skipped []string `json:"skipped"`
}

func (h *Host) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeError(w, http.StatusRequestEntityTooLarge, "request too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.stopped:
		writeError(w, http.StatusServiceUnavailable, "the server is shutting down")
		return
	case h.srv != nil || h.hasProjectFile():
		writeJSON(w, http.StatusConflict, apiError{Error: "this folder already has a project", Code: CodeProjectExists})
		return
	}
	res, err := scaffold.Create(h.opts.Dir, scaffold.Options{Name: req.Name, Force: req.Force})
	switch {
	case errors.Is(err, scaffold.ErrInvalidName):
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error(), Code: CodeInvalidName})
		return
	case errors.Is(err, scaffold.ErrNotEmpty):
		writeJSON(w, http.StatusConflict, apiError{Error: "the folder is not empty; add the project files to the existing ones, " +
			"or use an empty folder", Code: CodeNotEmpty})
		return
	case err != nil:
		h.warnf("cannot create a project in %s: %v", h.opts.Dir, err)
		writeError(w, http.StatusInternalServerError, "cannot create the project files (see the server log)")
		return
	}
	if err := h.open(); err != nil {
		h.warnf("cannot open the new project in %s: %v", h.opts.Dir, err)
		writeError(w, http.StatusInternalServerError, "the project was created but cannot be opened (see the server log)")
		return
	}
	h.logf("created project %q in %s; serving it now", res.Name, h.opts.Dir)
	writeJSON(w, http.StatusCreated, createResponse{Name: res.Name, Dir: h.opts.Dir, Created: res.Created, Skipped: append([]string{}, res.Skipped...)})
}

// handleHealth answers GET /health while there is no project: "degraded" with the check
// "project", so that monitoring sees a server that runs nothing.
func (h *Host) handleHealth(w http.ResponseWriter, _ *http.Request) {
	now := time.Now()
	hl := Health{Status: HealthOK, Version: version.Get().Version, StartedAt: h.started, UptimeSeconds: int64(now.Sub(h.started) / time.Second),
		Project: HealthProject{Dir: h.opts.Dir}, Checks: []HealthCheck{}}
	hl.Scheduler.Problems = []Problem{}
	check := func(name, status, msg string) {
		hl.Checks = append(hl.Checks, HealthCheck{Name: name, Status: status, Message: msg})
		if rank(status) > rank(hl.Status) {
			hl.Status = status
		}
	}
	check("project", HealthDegraded, "no project in "+h.opts.Dir+"; create one in the editor or with runnel init")
	healthDisk(&hl, dataDir(h.opts.HistoryDir, h.opts.Dir), diskFree, check)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, hl)
}

// StartTriggers runs the scheduler of the project's triggers until ctx is done; for a
// project created later, from its creation.
func (h *Host) StartTriggers(ctx context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ctx = ctx
	if h.srv != nil {
		h.srv.StartTriggers(ctx)
	}
}

// stop stops opening a project and returns the open one, if any.
func (h *Host) stop() *Server {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopped = true
	return h.srv
}

// Drain is Server.Drain of the project, if one is open.
func (h *Host) Drain(ctx context.Context) error {
	if srv := h.stop(); srv != nil {
		return srv.Drain(ctx)
	}
	return nil
}

// Shutdown is Server.Shutdown of the project, if one is open.
func (h *Host) Shutdown(ctx context.Context) error {
	if srv := h.stop(); srv != nil {
		return srv.Shutdown(ctx)
	}
	return nil
}

// Running returns the number of running runs.
func (h *Host) Running() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.srv == nil {
		return 0
	}
	return h.srv.Running()
}

// Close releases the project, if one is open.
func (h *Host) Close() error {
	if srv := h.stop(); srv != nil {
		return srv.Close()
	}
	return nil
}

func (h *Host) logf(format string, args ...any) {
	if h.opts.Log != nil {
		h.opts.Log(fmt.Sprintf(format, args...))
	}
}

func (h *Host) warnf(format string, args ...any) {
	if h.opts.Warn != nil {
		h.opts.Warn(fmt.Sprintf(format, args...))
	} else {
		h.logf(format, args...)
	}
}
