package core

import (
	"context"
	"fmt"

	"github.com/runnelhq/runnel/internal/action"
)

// core.success and core.abort end the workflow immediately; they share one
// implementation but have configurations of their own, so that the editor can
// describe the message field differently.
var (
	successDefinition = action.Definition{
		Type:        "core.success",
		Category:    action.CategoryGeneral,
		Label:       "Success",
		Description: "Ends the workflow successfully",
		Example:     "message: Nothing to import today\n",
		Config:      successConfig{},
		New: func(with map[string]any) (action.Action, error) {
			var c successConfig
			if err := action.DecodeConfig(with, &c); err != nil {
				return nil, err
			}
			return &end{kind: action.EndSuccess, message: c.Message}, nil
		},
	}
	abortDefinition = action.Definition{
		Type:        "core.abort",
		Category:    action.CategoryGeneral,
		Label:       "Abort",
		Description: "Ends the workflow as failed",
		Example:     "message: \"No orders found in ${INBOX}\"\n",
		Config:      abortConfig{},
		New: func(with map[string]any) (action.Action, error) {
			var c abortConfig
			if err := action.DecodeConfig(with, &c); err != nil {
				return nil, err
			}
			return &end{kind: action.EndFailure, message: c.Message}, nil
		},
	}
)

type successConfig struct {
	Message string `json:"message" doc:"Message for the run log"`
}

type abortConfig struct {
	Message string `json:"message" doc:"Reason for the run log; if empty, a default text names the action"`
}

type end struct {
	kind    action.End
	message string
}

func (a *end) Execute(_ context.Context, ec *action.ExecContext) (action.Result, error) {
	msg := a.message
	if msg == "" && a.kind == action.EndFailure {
		msg = fmt.Sprintf("aborted by action %q", ec.ActionID)
	}
	return action.Result{End: a.kind, Message: msg}, nil
}
