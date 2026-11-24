package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/runs"
)

// testTimeout limits a test of a connection (ADR 0022, section 8).
var testTimeout = 15 * time.Second

// testRequest names the environment whose variables and secrets the test uses; empty:
// the default environment of the project.
type testRequest struct {
	Environment string `json:"environment"`
}

// testResult answers POST /connections/{name}/test. A failed test is an answer too
// (200 with ok false): the message says what went wrong, without values of secrets.
type testResult struct {
	OK          bool   `json:"ok"`
	Message     string `json:"message"`
	Environment string `json:"environment,omitempty"`
	DurationMS  int64  `json:"durationMs"`
}

// handleTestConnection tests a saved connection of the project (S39c1): only files of
// the project, never hosts sent with the request; at most one test per connection at a
// time and testTimeout for each.
func (s *Server) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req testRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	p, err := model.LoadProject(s.dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "the project cannot be read; see Problems")
		return
	}
	if p.Connections[name] == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("connection %q not found, or its file has errors (see Problems)", name))
		return
	}
	env := req.Environment
	if env == "" {
		env = p.DefaultEnvironment
	}
	if env != "" && p.Environments[env] == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("environment %q not found", env))
		return
	}
	if _, busy := s.testing.LoadOrStore(name, true); busy {
		writeError(w, http.StatusConflict, fmt.Sprintf("connection %s is being tested already", name))
		return
	}
	defer s.testing.Delete(name)

	ctx, cancel := context.WithTimeout(paths.WithBase(r.Context(), s.dir), testTimeout)
	defer cancel()
	start := time.Now()
	res := testResult{Environment: env}
	c, mask, err := runs.ResolveConnection(ctx, p, env, name)
	if err == nil {
		res.Message, err = runs.TestConnection(ctx, s.dir, c, mask)
	}
	res.DurationMS = time.Since(start).Milliseconds()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		res.Message = fmt.Sprintf("no answer within %s", testTimeout)
	case err != nil:
		res.Message = err.Error()
	default:
		res.OK = true
	}
	// Secrets and values of ${env:…} never leave in a message.
	if mask != nil {
		res.Message = mask.String(res.Message)
	}
	writeJSON(w, http.StatusOK, res)
}
