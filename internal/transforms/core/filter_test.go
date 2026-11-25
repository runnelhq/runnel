package core

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

func orders() []record.Record {
	order := func(id int64, amount string, country string, day record.Date) record.Record {
		var r record.Record
		r.Set("id", id)
		if amount != "" {
			r.Set("amount", decimal.RequireFromString(amount))
		} else {
			r.Set("amount", nil)
		}
		r.Set("country", country)
		r.Set("day", day)
		return r
	}
	d := func(day int) record.Date { return record.DateOf(time.Date(2026, 11, day, 0, 0, 0, 0, time.UTC)) }
	return []record.Record{
		order(1, "1234.50", "DE", d(18)),
		order(2, "0.10", "AT", d(19)),
		order(3, "", "DE", d(19)),
		order(4, "999.99", "FR", d(20)),
	}
}

func filterIDs(t *testing.T, cond string, values map[string]any) ([]int64, []transformtest.Logged) {
	t.Helper()
	tr := transformtest.New(t, filterDefinition, map[string]any{"condition": cond})
	out, logs, err := transformtest.RunWithValues(context.Background(), tr, values, orders()...)
	if err != nil {
		t.Fatalf("%s: %v", cond, err)
	}
	var ids []int64
	for _, r := range out {
		v, _ := r.Get("id")
		ids = append(ids, v.(int64))
	}
	return ids, logs
}

func TestFilter(t *testing.T) {
	values := map[string]any{"params": map[string]any{"MIN": int64(500)}, "vars": map[string]any{}, "env": map[string]any{}, "run": map[string]any{}}
	for _, tt := range []struct{ cond, want string }{
		{"{{ record.amount > 0 }}", "[1 2 4]"},          // an empty amount is not more than 0
		{"record.amount >= params.MIN", "[1 4]"},        // without braces, with a parameter
		{"{{ record.amount == 0.1 }}", "[2]"},           // exact decimal
		{"{{ record.amount * 1.19 > 1189.99 }}", "[1]"}, // 999.99 * 1.19 = 1189.9881
		{"{{ record.country in ['DE', 'AT'] }}", "[1 2 3]"},
		{"{{ record.amount == nil }}", "[3]"},
		{"{{ record.day >= parseDate('19.11.2026', 'dd.MM.yyyy') }}", "[2 3 4]"},
		{"{{ formatDate(record.day, 'dd.MM.') == '20.11.' }}", "[4]"},
		{"{{ false }}", "[]"},
	} {
		if ids, _ := filterIDs(t, tt.cond, values); fmt.Sprint(ids) != tt.want {
			t.Errorf("%s: ids %v, want %s", tt.cond, ids, tt.want)
		}
	}
	_, logs := filterIDs(t, "{{ record.amount > 0 }}", nil)
	if len(logs) != 1 || logs[0].Msg != "kept 3 of 4 records, dropped 1" {
		t.Errorf("log %v", logs)
	}
}

func TestFilterErrors(t *testing.T) {
	for _, tt := range []struct{ cond, want string }{
		{"{{ record.amount > }}", "condition: expression {{ record.amount > }}"},
		{"{{ recrd.amount > 0 }}", "unknown name recrd"},
		{"{{ record.amount > 0", `missing closing "}}"`},
	} {
		if _, err := filterDefinition.New(map[string]any{"condition": tt.cond}); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error %v, want %q", tt.cond, err, tt.want)
		}
	}
	// Variables are resolved before the run; runnel validate checks the rest.
	if _, err := filterDefinition.New(map[string]any{"condition": "{{ record.amount > ${MIN} }}"}); err != nil {
		t.Errorf("with a variable: %v", err)
	}
	for _, tt := range []struct{ cond, want string }{
		{"{{ record.amount }}", "record 1: expression {{ record.amount }}: the result must be true or false, got 1234.5"},
		{"{{ record.country > 1 }}", "record 1: expression {{ record.country > 1 }}: invalid operation: string > int"},
	} {
		tr := transformtest.New(t, filterDefinition, map[string]any{"condition": tt.cond})
		if _, _, err := transformtest.Run(context.Background(), tr, orders()...); err == nil || err.Error() != tt.want {
			t.Errorf("%s: error %v, want %q", tt.cond, err, tt.want)
		}
	}
}

func TestFilterSchema(t *testing.T) {
	tr := transformtest.New(t, filterDefinition, map[string]any{"condition": "{{ record.amont > 0 && record.country == 'DE' }}"})
	in := record.Schema{}.With("amount", record.Decimal).With("country", record.String)
	ic := transform.NewInitContext(in)
	s, err := tr.(transform.Initializer).Init(ic)
	if err != nil || len(s.Names()) != 2 {
		t.Errorf("schema %+v %v, want the incoming one", s, err)
	}
	if p := ic.Problems(); len(p) != 1 || p[0].Path != "condition" || !strings.Contains(p[0].Message, `field "amont" does not exist in the incoming records (did you mean "amount"?)`) {
		t.Errorf("problems %+v", p)
	}
}

func TestFilterErrorLink(t *testing.T) {
	tr := transformtest.New(t, filterDefinition, map[string]any{"condition": "{{ record.country > 'B' }}"})
	in := []record.Record{
		record.New(record.Field{Name: "country", Value: "DE"}),
		record.New(record.Field{Name: "country", Value: int64(7)}),
		record.New(record.Field{Name: "country", Value: "AT"}),
	}
	out, failed, logs, err := transformtest.RunWithErrorLink(context.Background(), tr, in...)
	if err != nil || len(out) != 1 || len(failed) != 1 {
		t.Fatalf("out %v, failed %v, %v", out, failed, err)
	}
	if v, _ := failed[0].Get("_error"); !strings.Contains(fmt.Sprint(v), "record 2: expression {{ record.country > 'B' }}: invalid operation: int > string") {
		t.Errorf("_error %v", v)
	}
	if len(logs) != 1 || logs[0].Msg != "kept 1 of 3 records, dropped 1, 1 with errors" {
		t.Errorf("log %v", logs)
	}
}

// With else the records for which the condition is false go to that transform, the
// others to the other data links; validate checks the target.
func TestFilterElse(t *testing.T) {
	tr := transformtest.New(t, filterDefinition, map[string]any{"condition": "{{ record.amount > 1 }}", "else": "rejected"})
	in := make(chan transform.Batch, 1)
	in <- transform.Batch(orders())
	close(in)
	got := map[string][]int64{}
	var logs []string
	out := transform.NewOutput("t", func(record.Record) error {
		t.Error("Emit with else")
		return nil
	}, func(_ slog.Level, m string) { logs = append(logs, m) }).WithTargetIDs([]string{"ok", "rejected"}).WithTargets(func(target string, r record.Record) error {
		v, _ := r.Get("id")
		got[target] = append(got[target], v.(int64))
		return nil
	})
	if err := tr.Process(context.Background(), in, out); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got["ok"], got["rejected"]) != "[1 4] [2 3]" || len(logs) != 1 || logs[0] != "kept 2 of 4 records, sent to rejected 2" {
		t.Errorf("%v %v", got, logs)
	}

	for _, tt := range []struct {
		targets []string
		want    string
	}{
		{[]string{"ok", "rejected"}, ""},
		{[]string{"ok", "rejectd"}, `error else: no data link leads to "rejected" (did you mean "rejectd"?) (links to: ok, rejectd)`},
		{[]string{"rejected"}, "warning else: the records that pass are dropped: only the link to the else transform leaves the filter"},
	} {
		ic := transform.NewInitContext(record.Schema{}.With("amount", record.Decimal))
		ic.Targets = tt.targets
		if _, err := tr.(transform.Initializer).Init(ic); err != nil {
			t.Fatal(err)
		}
		var problems []string
		for _, p := range ic.Problems() {
			kind := "error"
			if p.Warning {
				kind = "warning"
			}
			problems = append(problems, fmt.Sprintf("%s %s: %s", kind, p.Path, p.Message))
		}
		if strings.Join(problems, "\n") != tt.want {
			t.Errorf("%v: %v", tt.targets, problems)
		}
	}
}

// BenchmarkFilter: 1000 records through the condition, without and with else.
func BenchmarkFilter(b *testing.B) {
	for _, with := range []map[string]any{
		{"condition": "{{ record.qty % 2 == 0 }}"},
		{"condition": "{{ record.qty % 2 == 0 }}", "else": "rejected"},
	} {
		b.Run(fmt.Sprintf("else=%v", with["else"] != nil), func(b *testing.B) {
			tr, err := newFilter(with)
			if err != nil {
				b.Fatal(err)
			}
			in := make([]record.Record, 1000)
			b.ReportAllocs()
			for b.Loop() {
				for i := range in {
					in[i] = item("pen", "1.10", int64(i))
				}
				if _, _, err := transformtest.RunRouted(context.Background(), tr, in...); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
