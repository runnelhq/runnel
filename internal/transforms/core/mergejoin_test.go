package core

import (
	"context"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

func joinWith(join string) map[string]any {
	return map[string]any{"left": "orders", "right": "customers", "join": join,
		"keys": []any{map[string]any{"left": "customerId", "right": "id"}}}
}

// Several records with the same key on both sides give every combination; empty keys
// match nothing; left and full keep the records without a partner.
func TestMergeJoin(t *testing.T) {
	orders := []record.Record{rec("o", int64(1), "customerId", int64(1)), rec("o", int64(2), "customerId", int64(2)),
		rec("o", int64(3), "customerId", int64(2)), rec("o", int64(4), "customerId", nil)}
	customers := []record.Record{rec("id", int64(2), "name", "B"), rec("id", int64(2), "name", "B2"),
		rec("id", int64(3), "name", "C"), rec("id", nil, "name", "nobody")}
	for join, want := range map[string]string{
		"inner": `{o: 2, customerId: 2, id: 2, name: "B"}|{o: 2, customerId: 2, id: 2, name: "B2"}|{o: 3, customerId: 2, id: 2, name: "B"}|{o: 3, customerId: 2, id: 2, name: "B2"}`,
		"left":  `{o: 1, customerId: 1}|{o: 2, customerId: 2, id: 2, name: "B"}|{o: 2, customerId: 2, id: 2, name: "B2"}|{o: 3, customerId: 2, id: 2, name: "B"}|{o: 3, customerId: 2, id: 2, name: "B2"}|{o: 4, customerId: null}`,
		"full":  `{o: 1, customerId: 1}|{o: 2, customerId: 2, id: 2, name: "B"}|{o: 2, customerId: 2, id: 2, name: "B2"}|{o: 3, customerId: 2, id: 2, name: "B"}|{o: 3, customerId: 2, id: 2, name: "B2"}|{customerId: 3, id: 3, name: "C"}|{o: 4, customerId: null}|{customerId: null, id: null, name: "nobody"}`,
	} {
		tr := transformtest.New(t, mergeJoinDefinition, joinWith(join))
		var copies []record.Record
		for _, r := range orders {
			copies = append(copies, r.Clone())
		}
		out, logs, err := transformtest.RunWithInputs(context.Background(), tr, map[string][]record.Record{"orders": copies, "customers": customers})
		if err != nil {
			t.Fatalf("%s: %v", join, err)
		}
		var got []string
		for _, r := range out {
			got = append(got, Format(r))
		}
		if strings.Join(got, "|") != want {
			t.Errorf("%s:\n%s\nwant\n%s", join, strings.Join(got, "|"), want)
		}
		if join == "full" && logs[0].Msg != "joined 4 left and 4 right records: 4 pairs, 2 left only, 2 right only" {
			t.Errorf("log %q", logs[0].Msg)
		}
	}
}

// Unsorted inputs and keys of different kinds stop the pipeline.
func TestMergeJoinErrors(t *testing.T) {
	run := func(orders, customers []record.Record) error {
		tr := transformtest.New(t, mergeJoinDefinition, joinWith("inner"))
		_, _, err := transformtest.RunWithInputs(context.Background(), tr, map[string][]record.Record{"orders": orders, "customers": customers})
		return err
	}
	err := run([]record.Record{rec("customerId", int64(1))}, []record.Record{rec("id", int64(3)), rec("id", int64(2))})
	if err == nil || err.Error() != "the records of customers are not sorted by id: record 2 (2) comes after 3; sort them before, e.g. with core.sort" {
		t.Errorf("unsorted: %v", err)
	}
	err = run([]record.Record{rec("customerId", int64(1))}, []record.Record{rec("id", "1")})
	if err == nil || err.Error() != "customerId (int) and id (string) cannot be compared; convert them to the same type, e.g. with core.convert" {
		t.Errorf("kinds: %v", err)
	}
	for want, with := range map[string]map[string]any{
		`left and right are both "a"`:   {"left": "a", "right": "a", "keys": []any{map[string]any{"left": "x"}}},
		"keys: name at least one field": {"left": "a", "right": "b", "keys": []any{}},
		`invalid join "right"`:          {"left": "a", "right": "b", "keys": []any{map[string]any{"left": "x"}}, "join": "right"},
	} {
		if _, err := mergeJoinDefinition.New(with); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: %v, want %q", with, err, want)
		}
	}
	// Outside the engine there are no inputs.
	tr := transformtest.New(t, mergeJoinDefinition, joinWith("inner"))
	if _, _, err := transformtest.Run(context.Background(), tr); err == nil || !strings.Contains(err.Error(), "no data link comes from") {
		t.Errorf("%v", err)
	}
}

// Init checks the inputs and keys and puts the right fields after the left ones.
func TestMergeJoinInit(t *testing.T) {
	tr := transformtest.New(t, mergeJoinDefinition, joinWith("left"))
	ic := transform.NewInitContext(record.Schema{})
	ic.Inputs = map[string]record.Schema{
		"orders":    record.Schema{}.With("o", record.Int).With("customerId", record.Int),
		"customers": record.Schema{}.With("id", record.Int).With("customerId", record.String).With("name", record.String),
		"other":     {},
	}
	s, _ := tr.(transform.Initializer).Init(ic)
	if got := strings.Join(s.Names(), ","); got != "o,customerId,id,name" {
		t.Errorf("schema %s", got)
	}
	if p := ic.Problems(); len(p) != 1 || !strings.Contains(p[0].Message, `the link from "other" is not used`) {
		t.Errorf("%+v", p)
	}
	ic = transform.NewInitContext(record.Schema{})
	ic.Inputs = map[string]record.Schema{"orders": {}, "x": {}}
	_, _ = tr.(transform.Initializer).Init(ic)
	if p := ic.Problems(); len(p) < 1 || p[0].Path != "right" && p[1].Path != "right" {
		t.Errorf("%+v", p)
	}
}

// BenchmarkMergeJoin joins 100,000 orders with 50,000 customers (two orders each).
func BenchmarkMergeJoin(b *testing.B) {
	orders := make([]record.Record, 100000)
	for i := range orders {
		orders[i] = rec("o", int64(i), "customerId", int64(i/2))
	}
	customers := make([]record.Record, 50000)
	for i := range customers {
		customers[i] = rec("id", int64(i), "name", "customer")
	}
	b.ReportAllocs()
	for b.Loop() {
		tr, _ := mergeJoinDefinition.New(joinWith("inner"))
		out, _, err := transformtest.RunWithInputs(context.Background(), tr, map[string][]record.Record{"orders": orders, "customers": customers})
		if err != nil || len(out) != 100000 {
			b.Fatal(len(out), err)
		}
	}
}
