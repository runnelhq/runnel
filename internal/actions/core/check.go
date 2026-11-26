package core

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
)

// core.check fails when a condition is false, with a message of its own, e.g. when an
// answer holds no orders. The failure link can report it; without one the workflow
// fails. Output: true.
var checkDefinition = action.Definition{
	Type:        "core.check",
	Category:    action.CategoryGeneral,
	Label:       "Check Condition",
	Description: "Fails with a message when a condition is false",
	Example:     "condition: \"{{ len(results.list.output) > 0 }}\"\nmessage: No order files in the inbox\n",
	Config:      checkAction{},
	New:         newCheck,
	Reference: action.Reference{
		Outputs: []action.OutputField{{Name: "output", Type: "boolean", Description: "true if the condition is true"}},
		Errors:  "Fails with the message (or \"the condition is false\") when the condition is false, and when it is neither true nor false; the failure link can report it.",
	},
}

type checkAction struct {
	Condition string `json:"condition" doc:"Expression that must be true, e.g. {{ results.fetch.output.status == 200 }}" schema:"required,example={{ results.fetch.output.status == 200 }}"`
	Message   string `json:"message" doc:"Message when the condition is false; empty: \"the condition is false\"" schema:"format=runnel-text,example=No order files in the inbox"`
}

func newCheck(with map[string]any) (action.Action, error) {
	a := &checkAction{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	if !strings.Contains(a.Condition, "{{") && !strings.Contains(a.Condition, "${") {
		if _, err := truth(a.Condition); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// truth reads the value of the condition: true or false.
func truth(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("condition: expected true or false, got %q (write an expression such as {{ x > 0 }})", s)
}

func (a *checkAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	ok, err := truth(a.Condition)
	if err != nil {
		return action.Result{}, err
	}
	if !ok {
		msg := strings.TrimRight(a.Message, "\r\n")
		if msg == "" {
			msg = "the condition is false"
		}
		return action.Result{Output: false}, fmt.Errorf("%s", msg)
	}
	ec.Logf(slog.LevelDebug, "the condition is true")
	return action.Result{Output: true}, nil
}
