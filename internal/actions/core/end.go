package core

import (
	"context"
	"fmt"

	"github.com/runnelhq/runnel/internal/action"
)

// core.success and core.abort end the workflow immediately; they share one implementation.
var (
	successDefinition = action.Definition{
		Type:        "core.success",
		Description: "Ends the workflow successfully",
		New:         newEnd(action.EndSuccess),
	}
	abortDefinition = action.Definition{
		Type:        "core.abort",
		Description: "Ends the workflow as failed",
		New:         newEnd(action.EndFailure),
	}
)

type end struct {
	kind    action.End
	Message string `json:"message"`
}

func newEnd(kind action.End) func(map[string]any) (action.Action, error) {
	return func(with map[string]any) (action.Action, error) {
		a := &end{kind: kind}
		return a, action.DecodeConfig(with, a)
	}
}

func (a *end) Execute(_ context.Context, ec *action.ExecContext) (action.Result, error) {
	msg := a.Message
	if msg == "" && a.kind == action.EndFailure {
		msg = fmt.Sprintf("aborted by action %q", ec.ActionID)
	}
	return action.Result{End: a.kind, Message: msg}, nil
}
