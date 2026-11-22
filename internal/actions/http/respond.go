package http

import (
	"context"
	"log/slog"

	"github.com/runnelhq/runnel/internal/action"
)

// http.respond answers the waiting caller of an http trigger with response: run
// (ADR 0018). The first http.respond of a run answers, later ones only log a warning;
// the workflow goes on after it. Without a waiting caller (started from the editor,
// the command line or another trigger) it does nothing, so that the workflow can be
// started any way. Output: sent (whether this was the answer) and status.
var respondDefinition = action.Definition{
	Type:        "http.respond",
	Category:    action.CategoryNetwork,
	Label:       "HTTP Response",
	Description: "Answers the caller of an HTTP trigger with status, headers and body; output: sent and status",
	Example:     "status: \"404\"\nbody:\n  error: \"customer {{ params.KDNR }} not found\"\n",
	Config:      respondAction{},
	New:         newRespond,
}

type respondAction struct {
	Status  string            `json:"status" doc:"HTTP status code, e.g. 200, 404 or 422" schema:"default=200,example=200"`
	Headers map[string]string `json:"headers" doc:"Headers of the answer, e.g. Content-Type"`
	Body    any               `json:"body" doc:"Body: a text is sent as text, other values (maps, lists, numbers) as JSON"`
}

func newRespond(with map[string]any) (action.Action, error) {
	a := &respondAction{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *respondAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	status, err := action.ParseStatus(a.Status)
	if err != nil {
		return action.Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	out := map[string]any{"sent": false, "status": status}
	if ec.Respond == nil {
		ec.Logf(slog.LevelDebug, "no HTTP caller waits for an answer; status %d not sent", status)
		return action.Result{Output: out}, nil
	}
	if !ec.Respond(action.Response{Status: status, Headers: a.Headers, Body: a.Body}) {
		ec.Logf(slog.LevelWarn, "the caller has already been answered; status %d not sent", status)
		return action.Result{Output: out}, nil
	}
	ec.Logf(slog.LevelInfo, "answered the caller with status %d", status)
	out["sent"] = true
	return action.Result{Output: out}, nil
}
