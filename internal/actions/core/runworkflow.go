package core

import (
	"context"
	"errors"

	"log/slog"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/expr"
)

// core.run-workflow runs another workflow of the project (a sub-workflow) and waits
// until it has finished. The called workflow gets only the parameters given here,
// plus the variables of the project and environment; its outputs become the output of
// the action, e.g. {{ results.import.output.ROWS }}. A failed workflow fails the action.
var runWorkflowDefinition = action.Definition{
	Type:        "core.run-workflow",
	Category:    action.CategoryExecution,
	Label:       "Run Workflow",
	Description: "Runs another workflow with parameters; output: its outputs",
	Example:     "workflow: workflows/import-region.workflow.yaml\nparams:\n  REGION: north\n",
	Config:      runWorkflow{},
	New:         newRunWorkflow,
}

type runWorkflow struct {
	Workflow string         `json:"workflow" doc:"Workflow file, relative to the project folder" schema:"required,example=workflows/import.workflow.yaml"`
	Params   map[string]any `json:"params" doc:"Values for the parameters of the workflow, by name"`
}

func newRunWorkflow(with map[string]any) (action.Action, error) {
	a := &runWorkflow{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *runWorkflow) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	if a.Workflow == "" {
		return action.Result{}, errors.New("no workflow given")
	}
	if ec.RunWorkflow == nil {
		return action.Result{}, errors.New("running other workflows is not available here")
	}
	// Parameters are text, like on the command line; the called workflow converts
	// them to the declared type. Numbers, dates and lists from expressions are formatted.
	params := make(map[string]string, len(a.Params))
	for name, v := range a.Params {
		params[name] = expr.Format(v)
	}
	outputs, err := ec.RunWorkflow(ctx, a.Workflow, params)
	if err != nil {
		if ctx.Err() != nil {
			return action.Result{}, ctx.Err()
		}
		return action.Result{}, err
	}
	if outputs == nil {
		outputs = map[string]any{}
	}
	ec.Logf(slog.LevelDebug, "%s returned %d output(s)", a.Workflow, len(outputs))
	return action.Result{Output: outputs}, nil
}
