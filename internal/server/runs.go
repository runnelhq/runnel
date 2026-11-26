package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/history"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/runs"
	"github.com/runnelhq/runnel/internal/triggers"
)

// startRequest starts a run of a saved workflow or pipeline file, or the target of a
// saved trigger file with its environment and parameters ("Run target now", E20a).
type startRequest struct {
	Path        string            `json:"path,omitempty"`
	Trigger     string            `json:"trigger,omitempty"`
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
	// Runs of the editor and the API start no error workflow, also not "Run target now" (ADR 0027).
	opts := startOptions{env: req.Environment, params: req.Params, noOnError: true}
	if req.Trigger != "" {
		if req.Path != "" || req.Environment != "" || len(req.Params) > 0 {
			writeError(w, http.StatusBadRequest, "a run of a trigger takes path, environment and params from the trigger")
			return
		}
		t, err := s.savedTrigger(req.Trigger)
		if diags := (model.Errors)(nil); errors.As(err, &diags) {
			writeJSON(w, http.StatusUnprocessableEntity, startProblems{Error: "the trigger has problems; fix them first", validateResponse: s.response(diags)})
			return
		}
		if err != nil {
			writeErr(w, err)
			return
		}
		req.Path = t.Target
		opts = startOptions{env: t.Environment, params: triggers.TriggerParams(t), trigger: t.Type + ":" + t.Name, noOnError: true}
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
	run, _, err := s.startFile(name, data, opts)
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

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := history.Filter{Status: q.Get("status"), Path: q.Get("path"), Trigger: q.Get("trigger"), Parent: q.Get("parent"),
		Before: q.Get("before")}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > runs.MaxList {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid limit %q (expected 1–%d)", v, runs.MaxList))
			return
		}
		f.Limit = n
	}
	list, err := s.runs.Find(r.Context(), f)
	if err != nil {
		// The runs in memory are still shown; the details go to the server log.
		s.warn(fmt.Sprintf("cannot read the run history: %v", err))
	}
	writeJSON(w, http.StatusOK, map[string][]runs.Run{"runs": list})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	d, ok, err := s.runs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.warn(fmt.Sprintf("cannot read the run history: %v", err))
		writeError(w, http.StatusInternalServerError, "the run history cannot be read; see the server log")
		return
	}
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

// savedTrigger reads a saved trigger file for "Run target now"; a trigger with problems
// is refused with them (422 like a workflow with problems).
func (s *Server) savedTrigger(path string) (*model.Trigger, error) {
	name, err := filePath(path)
	if err != nil {
		return nil, err
	}
	if fileKind(name) != "trigger" {
		return nil, errorf(http.StatusBadRequest, "trigger must be a trigger file (triggers/*.yaml)")
	}
	data, err := s.readFile(name)
	if err != nil {
		return nil, err
	}
	return model.ParseTrigger(name, data)
}

// startTrigger starts a run of a trigger's target (the saved file), with its
// environment and parameters (S37); extra are set by the trigger itself, e.g. the
// path of a new file (S38).
func (s *Server) startTrigger(t *model.Trigger, r triggers.StartRequest) (triggers.Started, error) {
	name := t.Target
	data, err := s.readFile(name)
	if err != nil {
		return triggers.Started{}, err
	}
	params := triggers.TriggerParams(t)
	if len(r.Params) > 0 && params == nil {
		params = map[string]string{}
	}
	maps.Copy(params, r.Params)
	rows, err := runs.Records(r.Rows)
	if err != nil {
		return triggers.Started{}, err
	}
	trigger := t.Type + ":" + t.Name
	run, pend, err := s.startFile(name, data, startOptions{env: t.Environment, params: params, trigger: trigger,
		client: r.Client, request: r.Request, respond: r.Respond, history: r.History, rows: rows})
	// A run that fails before it starts (e.g. a missing secret) is reported too (ADR 0027).
	if f, ok := runs.StartFailure(s.dir, name, data, trigger, t.Environment, false, err); ok {
		s.onError.Add(f)
	}
	if err != nil {
		return triggers.Started{}, err
	}
	return triggers.Started{ID: run.ID, Wait: func(ctx context.Context) (triggers.Outcome, bool) { return s.waitRun(ctx, pend) }}, nil
}

// runOnError runs the error workflow of a failed run and waits until it has ended (ADR
// 0027); problems go to the server log.
func (s *Server) runOnError(f runs.Failure) {
	data, err := s.readFile(f.Target)
	if err == nil {
		spec := runs.ErrorSpec(f, s.dir, filepath.Join(s.dir, filepath.FromSlash(f.Target)), data)
		spec.Registry, spec.Transforms = s.registry, s.transforms
		var run runs.Run
		if run, _, err = s.runs.Start(f.Target, spec); err == nil {
			s.info(fmt.Sprintf("on error: started %s for %s, run %s", f.Target, f.Describe(), run.ID))
			d, _ := s.runs.Wait(context.Background(), run.ID)
			if d.Status != workflow.Succeeded {
				s.warn(fmt.Sprintf("on error: %s (run %s) %s: %s", f.Target, run.ID, d.Status, d.Error))
			}
			return
		}
	}
	s.warn(fmt.Sprintf("on error: %s could not be started for %s: %v", f.Target, f.Describe(), err))
}

// startOptions describe a start of a saved file, from the API or a trigger.
type startOptions struct {
	env, trigger, client, history string
	// noOnError: no error workflow when the run fails (runs of the editor, ADR 0027).
	noOnError bool
	params    map[string]string
	request   map[string]any  // workflows (http triggers)
	rows      []record.Record // pipelines (JSON list of an http request)
	respond   action.Responder
}

// startFile starts a run of the saved workflow or pipeline name of the project. The
// pending run is the handle to wait for it (runs.Manager.WaitLaunched).
func (s *Server) startFile(name string, data []byte, o startOptions) (runs.Run, *runs.Pending, error) {
	var pend *runs.Pending
	var err error
	if fileKind(name) == "pipeline" {
		pend, _, err = s.runs.PreparePipelineRun(name, runs.PipelineSpec{Name: name, Data: data, ProjectDir: s.dir,
			Env: o.env, Params: o.params, Registry: s.transforms, Trigger: o.trigger, Client: o.client,
			Rows: o.rows, Respond: o.respond, History: o.history, NoOnError: o.noOnError})
	} else {
		pend, _, err = s.runs.PrepareRun(name, runs.Spec{Name: name, File: filepath.Join(s.dir, filepath.FromSlash(name)), Data: data,
			ProjectDir: s.dir, Env: o.env, Params: o.params, Registry: s.registry, Transforms: s.transforms, Trigger: o.trigger,
			Client: o.client, Request: o.request, Respond: o.respond, History: o.history, NoOnError: o.noOnError})
	}
	if err != nil {
		return runs.Run{}, nil, err
	}
	run, err := s.runs.Launch(pend)
	if err != nil {
		return runs.Run{}, nil, err
	}
	return run, pend, nil
}

// waitRun waits until the run of pend has ended (or ctx is done) for the default answer
// of an http trigger with response: run (S38b1): the outputs of a workflow, the counts of
// the transforms of a pipeline. It waits on the launched run, not on its id: with
// history errors or none, a run that ends at once is no longer found by its id.
func (s *Server) waitRun(ctx context.Context, pend *runs.Pending) (triggers.Outcome, bool) {
	d, ok := s.runs.WaitLaunched(ctx, pend)
	if !ok {
		return triggers.Outcome{}, false
	}
	out := triggers.Outcome{Status: string(d.Status), Error: d.Error}
	if d.Kind == "pipeline" {
		counts := map[string]any{}
		for _, a := range d.Actions {
			if a.Counts != nil {
				counts[a.ID] = a.Counts
			}
		}
		out.Body = map[string]any{"transforms": counts}
	} else if d.Outputs != nil {
		out.Body = d.Outputs
	}
	return out, true
}

// running reports whether a run is still running.
func (s *Server) running(id string) bool {
	return s.runs.IsRunning(id)
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
