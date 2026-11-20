package core

import (
	"context"
	"errors"
	"log/slog"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/expr"
)

// core.run-pipeline runs a pipeline of the project and waits until it has finished,
// like core.run-workflow (roadmap S18). The pipeline gets only the parameters given here,
// plus the variables of the project and environment. The output are the counts of its
// transforms, e.g. {{ results.load.output.transforms.write.written }}, and the number
// of records that went to error links ({{ results.load.output.errors }}). A failed
// pipeline fails the action.
var runPipelineDefinition = action.Definition{
	Type:        "core.run-pipeline",
	Category:    action.CategoryExecution,
	Label:       "Run Pipeline",
	Description: "Runs a pipeline with parameters; output: the counts of its transforms",
	Config:      runPipeline{},
	New:         newRunPipeline,
}

type runPipeline struct {
	Pipeline string         `json:"pipeline" doc:"Pipeline file, relative to the project folder" schema:"required,example=pipelines/import.pipeline.yaml"`
	Params   map[string]any `json:"params" doc:"Values for the parameters of the pipeline, by name"`
}

func newRunPipeline(with map[string]any) (action.Action, error) {
	a := &runPipeline{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *runPipeline) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	if a.Pipeline == "" {
		return action.Result{}, errors.New("no pipeline given")
	}
	if ec.RunPipeline == nil {
		return action.Result{}, errors.New("running pipelines is not available here")
	}
	// Parameters are text, like on the command line; the pipeline converts them.
	params := make(map[string]string, len(a.Params))
	for name, v := range a.Params {
		params[name] = expr.Format(v)
	}
	out, err := ec.RunPipeline(ctx, a.Pipeline, params)
	if err != nil {
		if ctx.Err() != nil {
			return action.Result{}, ctx.Err()
		}
		return action.Result{}, err
	}
	ec.Logf(slog.LevelDebug, "%s finished, %v record(s) with errors", a.Pipeline, out["errors"])
	return action.Result{Output: out}, nil
}
