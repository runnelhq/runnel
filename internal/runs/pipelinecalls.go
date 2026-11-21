package runs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"slices"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/engine/pipeline"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// Calls of pipelines (core.run-pipeline, roadmap S18), like calls of workflows: the
// pipeline runs in the same project and environment, gets only the parameters given by
// the action (undeclared ones are an error) and returns the counts of its transforms.
// Its log goes to the log of the calling action, prefixed with "<pipeline>/<transform>: ".

// ActionTypeRunPipeline is the type of the action that calls a pipeline.
const ActionTypeRunPipeline = "core.run-pipeline"

// pipelines returns the runner for pipelines called by the run of spec; nil if the
// spec has no transform types.
func (spec Spec) pipelines(envName string) action.PipelineRunner {
	if spec.Transforms == nil {
		return nil
	}
	return func(ctx context.Context, call action.PipelineCall) (map[string]any, error) {
		file, name := Resolve(spec.ProjectDir, spec.file(), call.Path)
		data, err := os.ReadFile(file)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("pipeline file %s not found", name)
		} else if err != nil {
			return nil, fmt.Errorf("cannot read pipeline file %s: %w", name, err)
		}
		rows, err := Records(call.Rows)
		if err != nil {
			return nil, err
		}
		p, err := PreparePipeline(PipelineSpec{
			Name: name, File: file, Data: data, ProjectDir: spec.ProjectDir, Env: envName, Params: call.Params,
			Registry: spec.Transforms, Observer: forwardPipeline(call.Log), Rows: rows,
		})
		if err == nil && len(p.Undeclared) > 0 {
			err = fmt.Errorf("parameter(s) %v not declared by pipeline %q (declared: %s)", p.Undeclared, p.Pipeline.Name, DeclaredOf(p.Pipeline.Parameters))
		}
		if err != nil {
			var diags model.Errors
			if errors.As(err, &diags) {
				return nil, fmt.Errorf("pipeline %s cannot be started: %s", name, onlyErrors(diags))
			}
			return nil, fmt.Errorf("pipeline %s cannot be started: %w", name, err)
		}
		res := p.Runner.Run(ctx)
		switch res.Status {
		case pipeline.Succeeded:
			return pipelineOutput(res), nil
		case pipeline.Cancelled:
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		return nil, fmt.Errorf("pipeline %s failed: %w", name, res.Err)
	}
}

// Records turns rows into records for core.from-result: the rows of core.run-pipeline
// or the JSON list in the body of a request to an http trigger. The fields of an object
// are ordered by name, as objects have no order.
func Records(rows []map[string]any) ([]record.Record, error) {
	if rows == nil {
		return nil, nil
	}
	out := make([]record.Record, len(rows))
	for i, row := range rows {
		for _, name := range slices.Sorted(maps.Keys(row)) {
			v, err := record.Normalize(row[name])
			if err != nil {
				return nil, fmt.Errorf("rows[%d].%s: %w", i, name, err)
			}
			out[i].Set(name, v)
		}
	}
	return out, nil
}

// pipelineOutput is the output of core.run-pipeline: the counts per transform, the
// number of records that went to error links and the result records (core.to-result),
// as expressions see them.
func pipelineOutput(res pipeline.Result) map[string]any {
	transforms := map[string]any{}
	var errs int64
	for id, c := range res.Counts {
		transforms[id] = map[string]any{"read": c.Read, "written": c.Written, "errors": c.Errors}
		errs += c.Errors
	}
	rows := make([]any, len(res.Rows))
	for i, r := range res.Rows {
		rows[i] = transform.RecordValues(r)
	}
	return map[string]any{"transforms": transforms, "errors": errs, "rows": rows}
}

// forwardPipeline writes the events of a called pipeline to the log of the calling
// action, with the words of the log of a pipeline run (DescribePipeline).
func forwardPipeline(log func(slog.Level, string)) func(pipeline.Event) {
	if log == nil {
		return nil
	}
	name := ""
	return func(e pipeline.Event) {
		kind := stepLine
		switch e.Kind {
		case pipeline.RunStarted:
			name, kind = e.Message, startLine
		case pipeline.RunFinished:
			kind = endLine
		case pipeline.Log:
			kind = logLine
		case pipeline.TransformStarted:
			return // not in the log of the calling action
		}
		if l, ok := DescribePipeline(name, "", e); ok {
			nested(log, name, l, kind)
		}
	}
}
