package core

import (
	"context"
	"log/slog"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// core.from-result passes on the records given to the run, e.g. with rows of
// core.run-pipeline (roadmap S24a; Hop: "Get rows from result"). Without such
// records, e.g. when the pipeline is started on its own, it passes on none.
var fromResultDefinition = transform.Definition{
	Type:        "core.from-result",
	Category:    transform.CategoryExecution,
	Label:       "Records from Workflow",
	Description: "Passes on the records the calling workflow gave to the pipeline (rows)",
	Config:      fromResult{},
	New:         newFromResult,
}

func newFromResult(with map[string]any) (transform.Transform, error) {
	t := &fromResult{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	return t, nil
}

type fromResult struct{}

// Init reports an open schema: the fields are only known when the pipeline runs.
func (*fromResult) Init(*transform.InitContext) (record.Schema, error) {
	return record.OpenSchema(), nil
}

func (*fromResult) Process(ctx context.Context, _ <-chan transform.Batch, out *transform.Output) error {
	rows := out.Rows()
	if rows == nil {
		out.Logf(slog.LevelInfo, "no records were given to the pipeline (rows of core.run-pipeline)")
	}
	for _, r := range rows {
		if err := out.Emit(r.Clone()); err != nil {
			return err
		}
	}
	return ctx.Err()
}
