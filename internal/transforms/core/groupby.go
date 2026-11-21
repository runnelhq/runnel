package core

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// core.group-by combines the records with the same values in the group fields into one
// record with aggregates (count, sum …). It groups in memory, in the order in which
// the groups first appear; memory grows with the number of groups, not of records.
var groupByDefinition = transform.Definition{
	Type:         "core.group-by",
	Category:     transform.CategoryCombine,
	Label:        "Group By",
	Description:  "Combines records with the same values into one, with count, sum and other aggregates",
	Config:       groupBy{},
	New:          newGroupBy,
	RecordErrors: true,
}

type groupBy struct {
	GroupBy    []string    `json:"groupBy" doc:"Fields whose values form a group; empty: one record for all"`
	Aggregates []aggregate `json:"aggregates" doc:"Fields computed per group, after the group fields"`
}

type aggregate struct {
	Name      string `json:"name" doc:"Name of the new field" schema:"required,example=total"`
	Function  string `json:"function" doc:"count: records (or values of field), countDistinct: different values, sum, avg, min, max, first, last, concat: values as text" schema:"required,enum=count|countDistinct|sum|avg|min|max|first|last|concat"`
	Field     string `json:"field" doc:"Field the function is applied to; empty for count: all records" schema:"example=amount"`
	Separator string `json:"separator" doc:"Text between the values of concat; empty: comma and space" schema:"example= / ,only=function:concat"`
}

var aggregateFunctions = []string{"count", "countDistinct", "sum", "avg", "min", "max", "first", "last", "concat"}

func newGroupBy(with map[string]any) (transform.Transform, error) {
	t := &groupBy{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	names := slices.Clone(t.GroupBy)
	for i, a := range t.Aggregates {
		at := fmt.Sprintf("aggregates[%d]", i)
		switch {
		case a.Name == "":
			return nil, fmt.Errorf("%s: every aggregate needs a name", at)
		case slices.Contains(names, a.Name):
			return nil, fmt.Errorf("%s: the field %q would exist twice", at, a.Name)
		case !slices.Contains(aggregateFunctions, a.Function):
			return nil, fmt.Errorf("%s: invalid function %q (expected %s)", at, a.Function, strings.Join(aggregateFunctions, ", "))
		case a.Field == "" && a.Function != "count":
			return nil, fmt.Errorf("%s: %s needs a field", at, a.Function)
		}
		names = append(names, a.Name)
	}
	return t, nil
}

// Init checks the fields and reports a closed schema: the group fields, then the
// aggregates with the types of their results.
func (t *groupBy) Init(ic *transform.InitContext) (record.Schema, error) {
	if len(t.GroupBy) == 0 && len(t.Aggregates) == 0 {
		ic.Error("", "list the group fields (groupBy) or aggregates")
	}
	var s record.Schema
	for i, f := range t.GroupBy {
		ic.Use(f, fmt.Sprintf("groupBy[%d]", i))
		def, _ := ic.In.Field(f)
		s = s.With(f, def.Type)
	}
	for i, a := range t.Aggregates {
		var in record.Type
		if a.Field != "" {
			ic.Use(a.Field, fmt.Sprintf("aggregates[%d].field", i))
			def, _ := ic.In.Field(a.Field)
			in = def.Type
			if a.Function == "sum" || a.Function == "avg" {
				if in != record.Unknown && in != record.Int && in != record.Decimal && in != record.Float {
					ic.Error(fmt.Sprintf("aggregates[%d].field", i), fmt.Sprintf("%s needs numbers, but %q is %s", a.Function, a.Field, in))
				}
			}
		}
		s = s.With(a.Name, resultType(a.Function, in))
	}
	return s, nil
}

// resultType is the type of an aggregate for a field of type in.
func resultType(function string, in record.Type) record.Type {
	switch function {
	case "count", "countDistinct":
		return record.Int
	case "concat":
		return record.String
	case "avg":
		if in == record.Int {
			return record.Decimal
		}
	}
	return in
}

// group holds the values of the group fields and the state of the aggregates.
type group struct {
	values []any
	state  []*aggState
}

type aggState struct {
	count    int64
	sum      any // int64, decimal.Decimal or float64; nil before the first value
	value    any // min, max, first, last
	seen     bool
	distinct map[string]bool
	texts    []string
}

func (t *groupBy) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	groups := map[string]*group{}
	var order []*group
	var n int64
	text, _ := format.ToText("")
	for b := range in {
		for _, r := range b {
			n++
			values := make([]any, len(t.GroupBy))
			var key strings.Builder
			for i, f := range t.GroupBy {
				values[i], _ = r.Get(f)
				key.WriteString(record.Key(values[i]))
				key.WriteByte(0)
			}
			// Check the values first: a record that does not fit changes no group.
			add := make([]any, len(t.Aggregates))
			for i, a := range t.Aggregates {
				if a.Field == "" {
					continue
				}
				v, _ := r.Get(a.Field)
				if err := check(a, v); err != nil {
					if err := out.Fail(r, a.Field, fmt.Errorf("record %d, field %q: %w", n, a.Field, err)); err != nil {
						return err
					}
					add = nil
					break
				}
				add[i] = v
			}
			if add == nil {
				continue
			}
			g := groups[key.String()]
			if g == nil {
				g = &group{values: values, state: make([]*aggState, len(t.Aggregates))}
				for i := range g.state {
					g.state[i] = &aggState{}
				}
				groups[key.String()] = g
				order = append(order, g)
			}
			for i, a := range t.Aggregates {
				if err := g.state[i].add(a, add[i], a.Field == "", text); err != nil {
					return fmt.Errorf("record %d, field %q: %w", n, a.Field, err)
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Without group fields, there is one record also for no input (count 0).
	if len(t.GroupBy) == 0 && len(order) == 0 {
		g := &group{state: make([]*aggState, len(t.Aggregates))}
		for i := range g.state {
			g.state[i] = &aggState{}
		}
		order = append(order, g)
	}
	out.Logf(slog.LevelInfo, "%d records in %d groups", n, len(order))
	for _, g := range order {
		r := record.Make(len(t.GroupBy) + len(t.Aggregates))
		for i, f := range t.GroupBy {
			r.Set(f, g.values[i])
		}
		for i, a := range t.Aggregates {
			r.Set(a.Name, g.state[i].result(a))
		}
		if err := out.Emit(r); err != nil {
			return err
		}
	}
	return nil
}

// check reports a value an aggregate cannot use.
func check(a aggregate, v any) error {
	k := record.KindOf(v)
	switch a.Function {
	case "sum", "avg":
		if k != record.KindNone && k != record.KindNumber {
			return fmt.Errorf("%s needs numbers, not %s values", a.Function, record.TypeOf(v))
		}
	case "min", "max":
		if k == record.Incomparable {
			return fmt.Errorf("%s values cannot be compared", record.TypeOf(v))
		}
	}
	return nil
}

func (s *aggState) add(a aggregate, v any, all bool, text format.Codec) error {
	switch a.Function {
	case "count":
		if all || v != nil {
			s.count++
		}
	case "first":
		if !s.seen {
			s.value, s.seen = v, true
		}
	case "last":
		s.value, s.seen = v, true
	}
	if v == nil {
		return nil // the other functions skip empty values, as in SQL
	}
	switch a.Function {
	case "countDistinct":
		if s.distinct == nil {
			s.distinct = map[string]bool{}
		}
		s.distinct[record.Key(v)] = true
	case "sum", "avg":
		s.count++
		s.sum = addNumbers(s.sum, v)
	case "min", "max":
		if s.value != nil && record.KindOf(s.value) != record.KindOf(v) {
			return fmt.Errorf("%s cannot compare %s with %s values", a.Function, record.TypeOf(v), record.KindOf(s.value))
		}
		c := 0
		if s.value != nil {
			c = record.Compare(v, s.value)
		}
		if s.value == nil || (a.Function == "min" && c < 0) || (a.Function == "max" && c > 0) {
			s.value = v
		}
	case "concat":
		t, err := text.Format(v)
		if err != nil {
			return err
		}
		s.texts = append(s.texts, t)
	}
	return nil
}

// addNumbers adds two numbers: int + int stays int, with a float the result is a
// float, otherwise a decimal (exact).
func addNumbers(sum, v any) any {
	if sum == nil {
		return v
	}
	x, xi := sum.(int64)
	y, yi := v.(int64)
	if xi && yi {
		return x + y
	}
	_, xd := sum.(decimal.Decimal)
	_, yd := v.(decimal.Decimal)
	if !xd && !yd {
		return toFloat(sum) + toFloat(v)
	}
	a, _ := record.ToDecimal(sum)
	b, _ := record.ToDecimal(v)
	return a.Add(b)
}

func toFloat(v any) float64 {
	switch v := v.(type) {
	case int64:
		return float64(v)
	case float64:
		return v
	}
	d, _ := record.ToDecimal(v)
	return d.InexactFloat64()
}

func (s *aggState) result(a aggregate) any {
	switch a.Function {
	case "count":
		return s.count
	case "countDistinct":
		return int64(len(s.distinct))
	case "sum":
		return s.sum // null without values, as in SQL
	case "avg":
		switch sum := s.sum.(type) {
		case nil:
			return nil
		case float64:
			return sum / float64(s.count)
		default:
			d, _ := record.ToDecimal(sum)
			return d.Div(decimal.NewFromInt(s.count))
		}
	case "concat":
		sep := a.Separator
		if sep == "" {
			sep = ", "
		}
		return strings.Join(s.texts, sep)
	}
	return s.value
}
