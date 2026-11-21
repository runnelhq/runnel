package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/runs"
	"github.com/runnelhq/runnel/internal/triggers"
)

// startRequest starts a run of a saved workflow or pipeline file.
type startRequest struct {
	Path        string            `json:"path"`
	Environment string            `json:"environment,omitempty"`
	Params      map[string]string `json:"params,omitempty"`
}

// startProblems is the response when a workflow cannot be started because of
// problems in its files.
type startProblems struct {
	Error string `json:"error"`
	validateResponse
}

func (s *Server) handleStartRun(w http.ResponseWriter, r *http.Request) {
	var req startRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	name, err := filePath(req.Path)
	if err != nil {
		writeErr(w, err)
		return
	}
	kind := fileKind(name)
	if kind != "workflow" && kind != "pipeline" {
		writeError(w, http.StatusBadRequest, "only workflow and pipeline files (*.workflow.yaml, *.pipeline.yaml) can be run")
		return
	}
	data, err := s.readFile(name)
	if err != nil {
		writeErr(w, err)
		return
	}
	var run runs.Run
	if kind == "pipeline" {
		run, _, err = s.runs.StartPipeline(name, runs.PipelineSpec{
			Name:       name,
			Data:       data,
			ProjectDir: s.dir,
			Env:        req.Environment,
			Params:     req.Params,
			Registry:   s.transforms,
		})
	} else {
		run, _, err = s.runs.Start(name, runs.Spec{
			Name:       name,
			File:       filepath.Join(s.dir, filepath.FromSlash(name)),
			Data:       data,
			ProjectDir: s.dir,
			Env:        req.Environment,
			Params:     req.Params,
			Registry:   s.registry,
			Transforms: s.transforms,
		})
	}
	var diags model.Errors
	switch {
	case errors.As(err, &diags):
		writeJSON(w, http.StatusUnprocessableEntity, startProblems{
			Error:            fmt.Sprintf("the %s cannot be started; fix the problems listed", kind),
			validateResponse: s.response(diags),
		})
	case errors.As(err, new(*runs.LimitError)):
		writeError(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, runs.ErrShutdown):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		w.Header().Set("Location", APIPrefix+"/runs/"+run.ID)
		writeJSON(w, http.StatusCreated, run)
	}
}

func (s *Server) handleListRuns(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string][]runs.Run{"runs": s.runs.Runs()})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	d, ok := s.runs.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	found, running := s.runs.Cancel(id)
	switch {
	case !found:
		writeError(w, http.StatusNotFound, "run not found")
	case !running:
		writeError(w, http.StatusConflict, "run has already finished")
	default:
		writeJSON(w, http.StatusAccepted, map[string]string{"id": id, "status": "cancelling"})
	}
}

// pingInterval keeps idle event streams open through proxies and detects closed clients.
var pingInterval = 15 * time.Second

// handleEvents streams run events as server-sent events. Each event has its sequence
// number as id; a client that reconnects with Last-Event-ID (or ?since=) receives the
// events it missed. If they are no longer available, it receives a "reset" event and
// should reload the runs. Without either, only new events are sent. ?run= limits the
// stream to one run.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	since := s.runs.LastSeq()
	for _, v := range []string{r.Header.Get("Last-Event-ID"), r.URL.Query().Get("since")} {
		if v == "" {
			continue
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid event id %q", v))
			return
		}
		since = n
	}
	runFilter := r.URL.Query().Get("run")

	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, "retry: 3000\n\n"); err != nil {
		return
	}

	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	for {
		events, complete, wake := s.runs.EventsSince(since)
		if !complete {
			if _, err := fmt.Fprint(w, "event: reset\ndata: {}\n\n"); err != nil {
				return
			}
		}
		for _, ev := range events {
			since = ev.Seq
			if runFilter != "" && ev.RunID != runFilter {
				continue
			}
			data, err := json.Marshal(ev)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Seq, ev.Kind, data); err != nil {
				return
			}
		}
		if err := rc.Flush(); err != nil {
			return
		}
		select {
		case <-wake:
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		case <-s.stop:
			return
		}
	}
}

// startTrigger starts a run of a trigger's target (the saved file), with its
// environment and parameters (S37); extra are set by the trigger itself, e.g. the
// path of a new file (S38).
func (s *Server) startTrigger(t *model.Trigger, extra map[string]string, client string) (string, error) {
	name := t.Target
	data, err := s.readFile(name)
	if err != nil {
		return "", err
	}
	trigger := t.Type + ":" + t.Name
	params := runs.TriggerParams(t)
	if len(extra) > 0 && params == nil {
		params = map[string]string{}
	}
	maps.Copy(params, extra)
	var run runs.Run
	if fileKind(name) == "pipeline" {
		run, _, err = s.runs.StartPipeline(name, runs.PipelineSpec{Name: name, Data: data, ProjectDir: s.dir,
			Env: t.Environment, Params: params, Registry: s.transforms, Trigger: trigger, Client: client})
	} else {
		run, _, err = s.runs.Start(name, runs.Spec{Name: name, File: filepath.Join(s.dir, filepath.FromSlash(name)), Data: data,
			ProjectDir: s.dir, Env: t.Environment, Params: params, Registry: s.registry, Transforms: s.transforms, Trigger: trigger, Client: client})
	}
	return run.ID, err
}

// running reports whether a run is still running.
func (s *Server) running(id string) bool {
	d, ok := s.runs.Get(id)
	return ok && d.Status == runs.StatusRunning
}

// triggersResponse lists the triggers of the project and the problems of trigger files.
type triggersResponse struct {
	Triggers []triggers.Info `json:"triggers"`
	// HTTP is the address of the listener of http triggers, once it is open (S38a).
	HTTP     string    `json:"http,omitempty"`
	Problems []Problem `json:"problems"`
}

func (s *Server) handleTriggers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, triggersResponse{Triggers: s.triggers.List(), HTTP: s.triggers.HTTPAddress(), Problems: s.response(s.triggers.Problems()).Problems})
}
