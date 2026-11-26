package core

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// core.to-result adds the records to the result of the pipeline and passes them on
// (roadmap S24a; Hop: "Copy rows to result"): core.run-pipeline returns them as
// output.rows. They are held in memory, at most maxRecords.
var toResultDefinition = transform.Definition{
	Type:        "core.to-result",
	Category:    transform.CategoryExecution,
	Label:       "Records to Workflow",
	Description: "Returns the records to the calling workflow (output.rows) and passes them on",
	Example:     "maxRecords: 1000\n",
	Config:      toResult{},
	New:         newToResult,
	Reference: action.Reference{
		Records: "Passes all records on unchanged and adds them to the result of the pipeline: core.run-pipeline returns them as output.rows.",
		Errors:  "Stops the pipeline when there are more records than maxRecords, or maxRecords is not a whole number of 1 or more.",
		Limits:  []string{"maxRecords limits the records held in memory for the result (default 100,000)."},
	},
}

// DefaultMaxResultRows limits the result records held in memory (ADR 0008).
const DefaultMaxResultRows = 100_000

type toResult struct {
	// MaxRecords is text, so that it can be a variable; checked when the pipeline runs.
	MaxRecords string `json:"maxRecords" doc:"Most records to return; more stop the pipeline, as they are held in memory" schema:"default=100000"`
}

func newToResult(with map[string]any) (transform.Transform, error) {
	t := &toResult{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *toResult) Process(_ context.Context, in <-chan transform.Batch, out *transform.Output) error {
	limit := int64(DefaultMaxResultRows)
	if s := strings.TrimSpace(t.MaxRecords); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 1 {
			return fmt.Errorf("maxRecords: %q is not a whole number of 1 or more", t.MaxRecords)
		}
		limit = n
	}
	var n int64
	for b := range in {
		for _, r := range b {
			if n++; n > limit {
				return fmt.Errorf("more than %d records for the result (maxRecords); they are held in memory – raise maxRecords or reduce the records", limit)
			}
			if err := out.AddResult(r.Clone()); err != nil {
				return err
			}
			if err := out.Emit(r); err != nil {
				return err
			}
		}
	}
	out.Logf(slog.LevelDebug, "%d records for the result", n)
	return nil
}

// Init passes the schema on: the records stay as they are.
func (t *toResult) Init(ic *transform.InitContext) (record.Schema, error) { return ic.In, nil }
