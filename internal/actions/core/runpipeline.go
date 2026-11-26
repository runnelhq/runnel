package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/expr"
)

// core.run-pipeline runs a pipeline of the project and waits until it has finished,
// like core.run-workflow (roadmap S18). The pipeline gets only the parameters given here,
// plus the variables of the project and environment. The output are the counts of its
// transforms, e.g. {{ results.load.output.transforms.write.written }}, and the number
// of records that went to error links ({{ results.load.output.errors }}). Records can
// be passed in (records, read by core.from-result) and come back (output.rows, written by
// core.to-result). A failed pipeline fails the action.
var runPipelineDefinition = action.Definition{
	Type:        "core.run-pipeline",
	Category:    action.CategoryExecution,
	Label:       "Run Pipeline",
	Description: "Runs a pipeline with parameters; output: the counts of its transforms",
	Example:     "pipeline: pipelines/customers.pipeline.yaml\nparams:\n  BATCH: \"42\"\n",
	Config:      runPipeline{},
	New:         newRunPipeline,
	Reference: action.Reference{
		Outputs: []action.OutputField{
			{Name: "output.transforms", Type: "object", Description: "Counts per transform id: read, written and errors"},
			{Name: "output.errors", Type: "integer", Description: "Records that went to error links, in all transforms"},
			{Name: "output.rows", Type: "list", Description: "The records that reached core.to-result, each as an object"},
		},
		Errors: "Fails when the pipeline stops with an error or cannot be started (file outside the project folder or missing, parameter not declared or invalid), when the pipeline is not in allowedTargets and when records is not a list. Records on error links do not fail the action.",
		Limits: []string{"output.rows holds at most maxRecords of core.to-result (default 100,000)."},
	},
}

type runPipeline struct {
	Pipeline string         `json:"pipeline" doc:"Pipeline file, relative to the project folder" schema:"required,example=pipelines/import.pipeline.yaml"`
	Params   map[string]any `json:"params" doc:"Values for the parameters of the pipeline, by name"`
	// AllowedTargets restricts a pipeline given by ${…} or {{ … }} (ADR 0008, Nachtrag S25b).
	AllowedTargets []string `json:"allowedTargets" doc:"Pipeline files this action may call, with wildcards and relative to the project folder, e.g. pipelines/import-*.pipeline.yaml; for a pipeline given by a variable or expression"`
	// Records is a list, usually from an expression; checked when the action runs. It was
	// rows until R5b.
	Records any `json:"records" doc:"Records for core.from-result in the pipeline: a list, e.g. {{ results.files.output }}; items that are not objects become a record with the field value"`
}

func newRunPipeline(with map[string]any) (action.Action, error) {
	a := &runPipeline{}
	if err := action.Renamed(with, "rows", "records (the records for core.from-result)"); err != nil {
		return nil, err
	}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	if err := checkTargets(a.AllowedTargets); err != nil {
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
	if err := allowed(a.Pipeline, a.AllowedTargets, "pipeline"); err != nil {
		return action.Result{}, err
	}
	if ec.RunPipeline == nil {
		return action.Result{}, errors.New("running pipelines is not available here")
	}
	// Parameters are text, like on the command line; the pipeline converts them.
	params := make(map[string]string, len(a.Params))
	for name, v := range a.Params {
		params[name] = expr.Format(v)
	}
	rows, err := toRows(a.Records)
	if err != nil {
		return action.Result{}, err
	}
	out, err := ec.RunPipeline(ctx, a.Pipeline, params, rows)
	if err != nil {
		if ctx.Err() != nil {
			return action.Result{}, ctx.Err()
		}
		return action.Result{}, err
	}
	ec.Logf(slog.LevelDebug, "%s finished, %v record(s) with errors", a.Pipeline, out["errors"])
	return action.Result{Output: out}, nil
}

// toRows turns the value of records into records: objects as they are, other items as
// {value: item}. Without records, nil.
func toRows(v any) ([]map[string]any, error) {
	switch v := v.(type) {
	case nil:
		return nil, nil
	case []any:
		rows := make([]map[string]any, len(v))
		for i, item := range v {
			if m, ok := item.(map[string]any); ok {
				rows[i] = m
			} else {
				rows[i] = map[string]any{"value": item}
			}
		}
		return rows, nil
	case []map[string]any:
		return v, nil
	}
	return nil, fmt.Errorf("records must be a list, e.g. {{ results.files.output }}, not %s", expr.Format(v))
}
