package core

import (
	"context"
	"fmt"
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
