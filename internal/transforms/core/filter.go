package core

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/model"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// core.filter passes on the records for which a condition is true; the others are
// dropped or, with else, go to a transform of their own (like "Send false data to" in
// Apache Hop).
var filterDefinition = transform.Definition{
	Parallel:     true,
	Type:         "core.filter",
	RecordErrors: true,
	Category:     transform.CategoryTransform,
	Label:        "Filter",
	Description:  "Passes on the records for which a condition is true",
	Example:      "condition: \"{{ record.umsatz > 0 && record.ort != '' }}\"\nelse: rejected\n",
	Config:       filter{},
	New:          newFilter,
	Reference: action.Reference{
		Records: "Passes the records for which the condition is true to the data links; the others go to the transform named in else, or are dropped and counted in the log.",
		Errors:  "A record whose condition cannot be evaluated goes to the error link; without one, the pipeline stops.",
	},
}

type filter struct {
	Condition string `json:"condition" doc:"Expression that must be true for a record to pass, e.g. {{ record.amount > 0 }}; a comparison with an empty value is false" schema:"required,example={{ record.amount > 0 }}"`
	Else      string `json:"else" doc:"Transform for the records for which the condition is false; a data link must lead there, and the records that pass go to the other data links. Empty: they are dropped" schema:"format=runnel-target,example=rejected"`

	cond *transform.Expression
	// pass are the targets of the records that pass when else is set: the other data links.
	pass []string
}

func newFilter(with map[string]any) (transform.Transform, error) {
	t := &filter{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	cond, err := transform.CompileExpression(t.Condition)
	if err != nil {
		return nil, fmt.Errorf("condition: %w", err)
	}
	t.cond = cond
	return t, nil
}

// Init checks the fields of the condition and passes the schema on: the records stay as
// they are.
func (t *filter) Init(ic *transform.InitContext) (record.Schema, error) {
	for _, f := range t.cond.Fields() {
		ic.Use(f, "condition")
	}
	if t.Else != "" {
		if !slices.Contains(ic.Targets, t.Else) {
			known := "none"
			if len(ic.Targets) > 0 {
				known = strings.Join(ic.Targets, ", ")
			}
			ic.Error("else", fmt.Sprintf("no data link leads to %q%s (links to: %s)", t.Else, model.Suggest(t.Else, ic.Targets), known))
		} else if len(ic.Targets) == 1 {
			ic.Warn("else", "the records that pass are dropped: only the link to the else transform leaves the filter")
		}
	}
	return ic.In, nil
}

func (t *filter) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	if t.Else != "" {
		t.pass = slices.DeleteFunc(slices.Clone(out.TargetIDs()), func(id string) bool { return id == t.Else })
	}
	var read, kept, failed int64
	for b := range in {
		for _, r := range b {
			read++
			ok, err := t.cond.Bool(out, r)
			if err != nil {
				// To an error link, or the pipeline stops.
				failed++
				if err := out.Fail(r, "", fmt.Errorf("record %d: %w", read, err)); err != nil {
					return err
				}
				continue
			}
			if !ok {
				if t.Else != "" {
					if err := out.EmitTo(t.Else, r); err != nil {
						return err
					}
				}
				continue
			}
			kept++
			if err := t.emit(out, r); err != nil {
				return err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	other := "dropped"
	if t.Else != "" {
		other = "sent to " + t.Else
	}
	if failed > 0 {
		out.Logf(slog.LevelInfo, "kept %d of %d records, %s %d, %d with errors", kept, read, other, read-kept-failed, failed)
	} else {
		out.Logf(slog.LevelInfo, "kept %d of %d records, %s %d", kept, read, other, read-kept)
	}
	return nil
}

// emit passes a record on: to all data links, or with else to all others; a record that
// goes to several of them is a copy for each but the last.
func (t *filter) emit(out *transform.Output, r record.Record) error {
	if t.Else == "" {
		return out.Emit(r)
	}
	for i, target := range t.pass {
		rec := r
		if i < len(t.pass)-1 {
			rec = r.Clone()
		}
		if err := out.EmitTo(target, rec); err != nil {
			return err
		}
	}
	return nil
}
