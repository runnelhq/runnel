package core

import (
	"context"
	"errors"

	"github.com/runnelhq/runnel/internal/action"
)

// core.start: entry point of every workflow; it has no configuration and always succeeds.
var startDefinition = action.Definition{
	Type:        "core.start",
	Category:    action.CategoryGeneral,
	Label:       "Start",
	Description: "Entry point of the workflow",
	New:         newStart,
	Reference:   action.Reference{Errors: "Never fails."},
}

type start struct{}

func newStart(with map[string]any) (action.Action, error) {
	if len(with) > 0 {
		return nil, errors.New("core.start has no configuration")
	}
	return start{}, nil
}

func (start) Execute(context.Context, *action.ExecContext) (action.Result, error) {
	return action.Result{}, nil
}
