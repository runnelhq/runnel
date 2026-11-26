package core

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

func countries(names ...any) []record.Record {
	var out []record.Record
	for i, n := range names {
		out = append(out, record.New(record.Field{Name: "id", Value: int64(i + 1)}, record.Field{Name: "country", Value: n}))
	}
	return out
}

func ids(rs []record.Record) string {
	var s []string
	for _, r := range rs {
		v, _ := r.Get("id")
		s = append(s, fmt.Sprint(v))
	}
	return strings.Join(s, " ")
}

var switchCases = []any{
	map[string]any{"when": "{{ record.country == 'DE' }}", "to": "germany"},
	map[string]any{"when": "record.country in ['DE', 'AT', 'FR']", "to": "eu"},
}

func TestSwitch(t *testing.T) {
	tr := transformtest.New(t, switchDefinition, map[string]any{"cases": switchCases, "else": "others"})
	out, logs, err := transformtest.RunRouted(context.Background(), tr, countries("DE", "AT", "US", "DE", nil, "FR")...)
	if err != nil {
		t.Fatal(err)
	}
	// The first case that is true decides: DE goes to germany only.
	for target, want := range map[string]string{"germany": "1 4", "eu": "2 6", "others": "3 5"} {
		if got := ids(out[target]); got != want {
			t.Errorf("%s: %s, want %s", target, got, want)
		}
	}
	if len(logs) != 1 || logs[0].Msg != "germany 2, eu 2, others 2" {
		t.Errorf("log %v", logs)
	}

	// Without else, records that match no case are dropped.
	tr = transformtest.New(t, switchDefinition, map[string]any{"cases": switchCases})
	out, logs, err = transformtest.RunRouted(context.Background(), tr, countries("DE", "US", "IT")...)
	if err != nil || ids(out["germany"]) != "1" || len(out["eu"]) != 0 || len(out[""]) != 0 {
		t.Errorf("without else: %v %v", out, err)
	}
	if len(logs) != 1 || logs[0].Msg != "germany 1, eu 0, dropped 2" {
		t.Errorf("log %v", logs)
	}
}

func TestSwitchErrors(t *testing.T) {
	cases := []any{map[string]any{"when": "{{ record.country > 'B' }}", "to": "late"}}
	tr := transformtest.New(t, switchDefinition, map[string]any{"cases": cases})
	_, _, err := transformtest.RunRouted(context.Background(), tr, countries("DE", int64(7))...)
	if err == nil || err.Error() != "record 2: case 1: expression {{ record.country > 'B' }}: invalid operation: int > string" {
		t.Errorf("without error link: %v", err)
	}
	_, failed, _, err := transformtest.RunWithErrorLink(context.Background(), tr, countries("DE", int64(7))...)
	if err != nil || len(failed) != 1 {
		t.Errorf("with error link: %v %v", failed, err)
	}

	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"cases": []any{}}, "cases: list at least one case"},
		{map[string]any{"cases": []any{map[string]any{"when": "{{ recrd.x }}", "to": "a"}}}, "cases[0].when: expression {{ recrd.x }}: unknown name recrd"},
		{map[string]any{"cases": []any{map[string]any{"when": "true"}}}, `missing required field "cases[0].to"`},
	} {
		if _, err := switchDefinition.New(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
}

func TestSwitchInit(t *testing.T) {
	tr := transformtest.New(t, switchDefinition, map[string]any{"else": "othrs", "cases": []any{
		map[string]any{"when": "{{ record.contry == 'DE' }}", "to": "germany"},
	}})
	ic := transform.NewInitContext(record.Schema{}.With("country", record.String))
	ic.Targets = []string{"germany", "eu", "others"}
	s, err := tr.(transform.Initializer).Init(ic)
	if err != nil || len(s.Fields) != 1 {
		t.Fatalf("schema %+v %v", s, err)
	}
	var got []string
	for _, p := range ic.Problems() {
		kind := "error"
		if p.Warning {
			kind = "warning"
		}
		got = append(got, fmt.Sprintf("%s %s: %s", kind, p.Path, p.Message))
	}
	want := []string{
		`error cases[0].when: field "contry" does not exist in the incoming records (did you mean "country"?) (fields: country)`,
		`error else: no data link leads to "othrs" (did you mean "others"?) (links to: germany, eu, others)`,
		`warning : the link to "eu" gets no records: no case and not else leads there`,
		`warning : the link to "others" gets no records: no case and not else leads there`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("problems:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// default of core.switch is else since R5b, as in core.filter; the old name is an unknown
// field.
func TestSwitchOldDefault(t *testing.T) {
	if _, err := newSwitch(map[string]any{"cases": switchCases, "default": "others"}); err == nil || !strings.Contains(err.Error(), `unknown field "default"`) {
		t.Errorf("%v", err)
	}
}
