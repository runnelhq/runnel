package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// core.sequence numbers the records in the order they arrive: a field with 1, 2, 3 … (or
// another start and step), e.g. a rank after core.sort or a position number. With
// groupBy every combination of values counts on its own (position 1, 2 … per order); the
// counters are kept in memory, one per group.
var sequenceDefinition = transform.Definition{
	Type:        "core.sequence",
	Category:    transform.CategoryTransform,
	Label:       "Sequence",
	Description: "Numbers the records in the order they arrive, also per group",
	Example:     "name: position\nstart: 10\nstep: 10\ngroupBy: [Nr]\n",
	Config:      sequence{},
	New:         newSequence,
	Reference: action.Reference{
		Outputs: []action.OutputField{{Name: "<name>", Type: "int", Description: "The number of the record, from start in steps of step"}},
		Records: "Passes every record on with the number, in the order they arrive.",
		Errors:  "Stops the pipeline when start or step is not a whole number.",
		Limits:  []string{"With groupBy, one counter per group is kept in memory."},
	},
}

type sequence struct {
	Name string `json:"name" doc:"Name of the field for the number; an existing field of the same name gets the number" schema:"required,example=position"`
	// Start and Step are text, so that they can be variables (${START}; number fields, R5b2).
	Start   string   `json:"start" doc:"Number of the first record, e.g. 1 or ${START}" schema:"default=1"`
	Step    string   `json:"step" doc:"Difference between two numbers, e.g. 10 for 10, 20, 30; may be negative" schema:"default=1"`
	GroupBy []string `json:"groupBy" doc:"Fields whose values form a group that counts on its own from start; empty: one count for all records" schema:"format=runnel-field"`

	start, step int64
}

func newSequence(with map[string]any) (transform.Transform, error) {
	t := &sequence{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	var err error
	if t.start, err = wholeNumber("start", t.Start, 1); err != nil {
		return nil, err
	}
	if t.step, err = wholeNumber("step", t.Step, 1); err != nil {
		return nil, err
	}
	if strings.TrimSpace(t.Name) == "" {
		return nil, fmt.Errorf("name: the field for the number needs a name")
	}
	for i, f := range t.GroupBy {
		if f == "" {
			return nil, fmt.Errorf("groupBy[%d]: empty field name", i)
		}
	}
	return t, nil
}

// Init checks the fields of groupBy and adds the number (int) to the schema.
func (t *sequence) Init(ic *transform.InitContext) (record.Schema, error) {
	for i, f := range t.GroupBy {
		ic.Use(f, fmt.Sprintf("groupBy[%d]", i))
	}
	return ic.In.With(t.Name, record.Int), nil
}

// wholeNumber reads start or step: a whole number, possibly from a variable; empty if it
// is not set (or not resolved yet).
func wholeNumber(field, s string, empty int64) (int64, error) {
	n, ok, err := transform.ParseWholeNumber(field, s)
	if err != nil || !ok {
		return empty, err
	}
	return n, nil
}

func (t *sequence) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	start, step := t.start, t.step
	next := start
	groups := map[string]int64{}
	var key strings.Builder
	for b := range in {
		for _, r := range b {
			n := next
			if len(t.GroupBy) == 0 {
				next += step
			} else {
				key.Reset()
				for _, f := range t.GroupBy {
					v, _ := r.Get(f)
					key.WriteString(strings.ReplaceAll(record.Key(v), "\x00", "\x00\x01"))
					key.WriteString("\x00\x00")
				}
				k := key.String()
				if last, ok := groups[k]; ok {
					n = last + step
				} else {
					n = start
				}
				groups[k] = n
			}
			r.Set(t.Name, n)
			if err := out.Emit(r); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}
