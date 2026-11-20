package core

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// core.switch passes each record to one of its outgoing links: the first case whose
// condition is true decides, else the default.
var switchDefinition = transform.Definition{
	Parallel:     true,
	Type:         "core.switch",
	Category:     transform.CategoryFlow,
	Label:        "Switch",
	Description:  "Passes each record to the link of the first case whose condition is true",
	Config:       switchRecords{},
	New:          newSwitch,
	RecordErrors: true,
}

type switchRecords struct {
	Cases   []switchCase `json:"cases" doc:"Conditions in order; the first one that is true decides" schema:"required"`
	Default string       `json:"default" doc:"Transform for records that match no case; empty: they are dropped" schema:"example=others"`

	conds []*transform.Expression
}

type switchCase struct {
	When string `json:"when" doc:"Condition, e.g. {{ record.country == 'DE' }}; a comparison with an empty value is false" schema:"required,example={{ record.amount > 0 }}"`
	To   string `json:"to" doc:"Id of the transform the record goes to; a data link must lead there" schema:"required,example=germany"`
}

func newSwitch(with map[string]any) (transform.Transform, error) {
	t := &switchRecords{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	if len(t.Cases) == 0 {
		return nil, fmt.Errorf("cases: list at least one case")
	}
	for i, c := range t.Cases {
		cond, err := transform.CompileExpression(c.When)
		if err != nil {
			return nil, fmt.Errorf("cases[%d].when: %w", i, err)
		}
		t.conds = append(t.conds, cond)
	}
	return t, nil
}

// Init checks the conditions and the targets: every "to" and the default must be reached
// by a data link, and a data link no case leads to gets a warning. The records stay as
// they are.
func (t *switchRecords) Init(ic *transform.InitContext) (record.Schema, error) {
	used := map[string]bool{}
	check := func(target, path string) {
		used[target] = true
		if !slices.Contains(ic.Targets, target) {
			known := "none"
			if len(ic.Targets) > 0 {
				known = strings.Join(ic.Targets, ", ")
			}
			ic.Error(path, fmt.Sprintf("no data link leads to %q%s (links to: %s)", target, model.Suggest(target, ic.Targets), known))
		}
	}
	for i, c := range t.Cases {
		for _, f := range t.conds[i].Fields() {
			ic.Use(f, fmt.Sprintf("cases[%d].when", i))
		}
		check(c.To, fmt.Sprintf("cases[%d].to", i))
	}
	if t.Default != "" {
		check(t.Default, "default")
	}
	for _, target := range ic.Targets {
		if !used[target] {
			ic.Warn("", fmt.Sprintf("the link to %q gets no records: no case and not the default leads there", target))
		}
	}
	return ic.In, nil
}

func (t *switchRecords) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	counts := make([]int64, len(t.Cases))
	var read, defaults, dropped, failed int64
	for b := range in {
	records:
		for _, r := range b {
			read++
			for i, cond := range t.conds {
				ok, err := cond.Bool(out, r)
				if err != nil {
					failed++
					if err := out.Fail(r, "", fmt.Errorf("record %d: case %d: %w", read, i+1, err)); err != nil {
						return err
					}
					continue records
				}
				if ok {
					counts[i]++
					if err := out.EmitTo(t.Cases[i].To, r); err != nil {
						return err
					}
					continue records
				}
			}
			if t.Default == "" {
				dropped++
				continue
			}
			defaults++
			if err := out.EmitTo(t.Default, r); err != nil {
				return err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Log per target, e.g. "germany 3, eu 2, others 1, dropped 0".
	perTarget := map[string]int64{}
	var order []string
	add := func(target string, n int64) {
		if _, ok := perTarget[target]; !ok {
			order = append(order, target)
		}
		perTarget[target] += n
	}
	for i, c := range t.Cases {
		add(c.To, counts[i])
	}
	if t.Default != "" {
		add(t.Default, defaults)
	}
	parts := make([]string, 0, len(order)+2)
	for _, target := range order {
		parts = append(parts, fmt.Sprintf("%s %d", target, perTarget[target]))
	}
	if t.Default == "" {
		parts = append(parts, fmt.Sprintf("dropped %d", dropped))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("errors %d", failed))
	}
	out.Logf(slog.LevelInfo, "%s", strings.Join(parts, ", "))
	return nil
}
