// Package transformtest helps testing transforms without the engine.
package transformtest

import (
	"context"
	"log/slog"
	"testing"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// Logged is a message a transform wrote to the run log.
type Logged struct {
	Level slog.Level
	Msg   string
}

// New creates a transform from a definition and fails the test on errors.
func New(t *testing.T, def transform.Definition, with map[string]any) transform.Transform {
	t.Helper()
	tr, err := def.New(with)
	if err != nil {
		t.Fatalf("%s: %v", def.Type, err)
	}
	return tr
}

// Run passes the input records (in one batch each) to a transform and returns the
// records it emitted, its log messages and its error.
func Run(ctx context.Context, tr transform.Transform, input ...record.Record) ([]record.Record, []Logged, error) {
	return RunWithValues(ctx, tr, nil, input...)
}

// RunWithValues is Run with the values of a run that expressions see besides the record
// (vars, params, env, run; see transform.Output.WithValues).
func RunWithValues(ctx context.Context, tr transform.Transform, values map[string]any, input ...record.Record) ([]record.Record, []Logged, error) {
	in := make(chan transform.Batch, len(input))
	for _, r := range input {
		in <- transform.Batch{r}
	}
	close(in)
	var out []record.Record
	var logs []Logged
	o := transform.NewOutput("t", func(r record.Record) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		out = append(out, r)
		return nil
	}, func(l slog.Level, m string) { logs = append(logs, Logged{l, m}) }).WithValues(values)
	err := tr.Process(ctx, in, o)
	return out, logs, err
}
