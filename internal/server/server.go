// Package server implements "runnel serve": the HTTP API under /api/v1 that the
// editor uses to read and write project files, list the available actions, validate
// workflows, start and cancel runs and follow them live (docs/dev/API.md).
//
// The server only listens on the loopback interface. It rejects requests whose
// Host header is not a loopback address (protection against DNS rebinding) and
// requests from other origins (protection against cross-site requests from web pages).
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"sync"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/httpx"
	"github.com/runnelhq/runnel/internal/runs"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/triggers"
	"github.com/runnelhq/runnel/internal/webui"
)

// APIPrefix is the path prefix of all API endpoints.
const APIPrefix = "/api/v1"

// Options configure a server.
type Options struct {
	// Dir is the project directory (containing project.yaml).
	Dir string
	// Registry holds the available actions.
	Registry *action.Registry
	// Transforms holds the available transforms; nil means none.
	Transforms *transform.Registry
	// UI holds the editor files; nil uses the editor embedded into the binary.
	UI fs.FS
	// Log receives messages for the server log (e.g. runs started by triggers); nil: none.
	Log func(msg string)
	// Warn receives problems for the server log (e.g. trigger files with errors); nil: Log.
	Warn func(msg string)
	// TriggerListen is the address of the listener of http triggers (S38a); empty: none.
	TriggerListen string
	// TriggerTLS makes it serve HTTPS (S40a); nil: plain HTTP.
	TriggerTLS *tls.Config
}

// Server serves the API for one project.
type Server struct {
	dir        string
	root       *os.Root
	registry   *action.Registry
	transforms *transform.Registry
	ui         fs.FS
	// writeMu serializes changes to files, so that the ETag check and the write
	// happen atomically with respect to other requests of this server.
	writeMu sync.Mutex
	runs    *runs.Manager
	// triggers starts the runs of the project's triggers (S37); see StartTriggers.
	triggers *triggers.Scheduler
	// stop is closed by Shutdown to end open event streams.
	stop     chan struct{}
	stopOnce sync.Once
}

// New opens the project directory. Close releases it.
func New(opts Options) (*Server, error) {
	root, err := os.OpenRoot(opts.Dir)
	if err != nil {
		return nil, err
	}
	ui := opts.UI
	if ui == nil {
		ui = webui.FS()
	}
	s := &Server{dir: opts.Dir, root: root, registry: opts.Registry, transforms: opts.Transforms, ui: ui, runs: runs.NewManager(),
		stop: make(chan struct{})}
	s.triggers = triggers.New(triggers.Options{Dir: opts.Dir, Start: s.startTrigger, Wait: s.waitRun, Running: s.running, Log: opts.Log, Warn: opts.Warn,
		Listen: opts.TriggerListen, TLS: opts.TriggerTLS})
	return s, nil
}

// StartTriggers runs the scheduler of the project's triggers until ctx is done (S37).
func (s *Server) StartTriggers(ctx context.Context) {
	go s.triggers.Run(ctx)
}

// Shutdown ends open event streams, cancels running runs and waits until they have
// ended or ctx is done. Call it before shutting down the HTTP server, which otherwise
// waits for the event streams.
func (s *Server) Shutdown(ctx context.Context) error {
	s.stopOnce.Do(func() { close(s.stop) })
	return s.runs.Shutdown(ctx)
}

// Drain stops starting runs (triggers and the API get "the server is shutting down",
// 503) and waits until the running runs have ended or ctx is done, without cancelling
// them (S41). Call Shutdown afterwards for the rest.
func (s *Server) Drain(ctx context.Context) error {
	return s.runs.Drain(ctx)
}

// Running returns the number of running runs.
func (s *Server) Running() int {
	return s.runs.Running()
}

// Close releases the project directory.
func (s *Server) Close() error {
	return s.root.Close()
}

// Handler returns the HTTP handler with all endpoints.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+APIPrefix+"/projects/current", s.handleProject)
	mux.HandleFunc("GET "+APIPrefix+"/projects/current/tree", s.handleTree)
	mux.HandleFunc("GET "+APIPrefix+"/files/{path...}", s.handleGetFile)
	mux.HandleFunc("PUT "+APIPrefix+"/files/{path...}", s.handlePutFile)
	mux.HandleFunc("DELETE "+APIPrefix+"/files/{path...}", s.handleDeleteFile)
	mux.HandleFunc("POST "+APIPrefix+"/folders", s.handleCreateFolder)
	mux.HandleFunc("DELETE "+APIPrefix+"/folders/{path...}", s.handleDeleteFolder)
	mux.HandleFunc("POST "+APIPrefix+"/rename", s.handleRename)
	mux.HandleFunc("GET "+APIPrefix+"/references/{path...}", s.handleReferences)
	mux.HandleFunc("GET "+APIPrefix+"/catalog", s.handleCatalog)
	mux.HandleFunc("GET "+APIPrefix+"/schemas/{kind}", s.handleSchema)
	mux.HandleFunc("GET "+APIPrefix+"/variables", s.handleVariables)
	mux.HandleFunc("GET "+APIPrefix+"/expressions", s.handleExpressions)
	mux.HandleFunc("POST "+APIPrefix+"/validate", s.handleValidate)
	mux.HandleFunc("POST "+APIPrefix+"/workflows/edit", s.handleEditWorkflow)
	mux.HandleFunc("POST "+APIPrefix+"/pipelines/edit", s.handleEditPipeline)
	mux.HandleFunc("POST "+APIPrefix+"/copy", s.handleCopy)
	mux.HandleFunc("POST "+APIPrefix+"/pipelines/fields", s.handleDetectFields)
	mux.HandleFunc("POST "+APIPrefix+"/preview", s.handlePreview)
	mux.HandleFunc("POST "+APIPrefix+"/runs", s.handleStartRun)
	mux.HandleFunc("GET "+APIPrefix+"/runs", s.handleListRuns)
	mux.HandleFunc("GET "+APIPrefix+"/runs/{id}", s.handleGetRun)
	mux.HandleFunc("POST "+APIPrefix+"/runs/{id}/cancel", s.handleCancelRun)
	mux.HandleFunc("GET "+APIPrefix+"/events", s.handleEvents)
	mux.HandleFunc("GET "+APIPrefix+"/triggers", s.handleTriggers)
	mux.HandleFunc("GET "+APIPrefix+"/version", handleVersion)
	mux.HandleFunc("/", s.handleUI)
	mux.HandleFunc(APIPrefix+"/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown endpoint %s %s", r.Method, r.URL.Path))
	})
	return guard(mux)
}

// guard rejects requests that do not come from a local client of this server.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !httpx.IsLoopback(r.Host) {
			writeError(w, http.StatusForbidden, "requests must be addressed to localhost or 127.0.0.1")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			writeError(w, http.StatusForbidden, "requests from other origins are not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// apiError is the body of all error responses.
type apiError = httpx.ErrorBody

// writeJSON writes v as indented JSON (readable when the API is used by hand).
func writeJSON(w http.ResponseWriter, status int, v any) { httpx.WriteJSON(w, status, v, true) }

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

// writeErr maps an error to a response; errors of type *httpError carry their status.
func writeErr(w http.ResponseWriter, err error) {
	var he *httpError
	if errors.As(err, &he) {
		writeError(w, he.status, he.msg)
		return
	}
	switch {
	case errors.Is(err, os.ErrNotExist):
		writeError(w, http.StatusNotFound, "file not found")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

type httpError struct {
	status int
	msg    string
}

func (e *httpError) Error() string { return e.msg }

func errorf(status int, format string, args ...any) error {
	return &httpError{status: status, msg: fmt.Sprintf(format, args...)}
}
