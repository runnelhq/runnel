package expr

import (
	"strings"
	"testing"
	"time"
)

func eval(t *testing.T, src string, env map[string]any) any {
	t.Helper()
	tpl, err := Compile(src, WorkflowRoots)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	v, err := tpl.Eval(env)
	if err != nil {
		t.Fatalf("Eval(%q): %v", src, err)
	}
	return v
}

func TestEvalKeepsTypeOfWholeValues(t *testing.T) {
	env := map[string]any{
		"result":  map[string]any{"rows": 42, "files": []any{"a.csv", "b.csv"}},
		"params":  map[string]any{"NAME": "Ada"},
		"vars":    map[string]any{},
		"env":     map[string]any{},
		"run":     map[string]any{"id": "01ABC"},
		"results": map[string]any{"load": map[string]any{"rows": 7}},
	}
	for _, tt := range []struct {
		src  string
		want any
	}{
		{"{{ result.rows > 0 }}", true},
		{"  {{ result.rows * 2 }} ", 84},
		{"{{ len(result.files) }}", 2},
		{`{{ results["load"].rows }}`, 7},
		{"Import {{ run.id }} done", "Import 01ABC done"},
		{"{{ result.rows }} rows, {{ result.files }}", `42 rows, ["a.csv","b.csv"]`},
		{"{{ upper(params.NAME) }}", "ADA"},
		{"{{ params.MISSING ?? 'none' }}", "none"},
		{"no expression", "no expression"},
		{"{{ 1.5 }}x", "1.5x"},
	} {
		if got := eval(t, tt.src, env); got != tt.want {
			t.Errorf("%q = %#v, want %#v", tt.src, got, tt.want)
		}
	}
}

func TestCompileErrors(t *testing.T) {
	for _, tt := range []struct{ src, want string }{
		{"{{ reslt.rows }}", "unknown name reslt"},
		{"a {{ 1 + }} b", "expression {{ 1 + }}"},
		{"{{ result.rows", `missing closing "}}"`},
		{"{{ }}", "the expression is empty"},
		{"{{ addDays(today()) }}", "not enough arguments"},
		{"{{ " + strings.Repeat("1+", 3000) + "1 }}", "longer than"},
	} {
		_, err := Compile(tt.src, WorkflowRoots)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("Compile(%q): %v, want containing %q", tt.src, err, tt.want)
		}
	}
	// The offset points to the "{{" of the expression.
	_, err := Compile("abc {{ 1 + }}", WorkflowRoots)
	if e, ok := IsError(err); !ok || e.Offset != 4 {
		t.Errorf("offset: %#v", err)
	}
}

func TestEvalBool(t *testing.T) {
	tpl, _ := Compile("{{ result.rows }}", WorkflowRoots)
	if _, err := tpl.EvalBool(map[string]any{"result": map[string]any{"rows": 3}}); err == nil ||
		!strings.Contains(err.Error(), "must be true or false, got 3") {
		t.Errorf("error %v", err)
	}
	tpl, _ = Compile("{{ result.rows >= 3 }}", WorkflowRoots)
	if ok, err := tpl.EvalBool(map[string]any{"result": map[string]any{"rows": 3}}); !ok || err != nil {
		t.Errorf("%v %v", ok, err)
	}
}

func TestFormat(t *testing.T) {
	d := time.Date(2026, 11, 19, 14, 30, 0, 0, time.UTC)
	for _, tt := range []struct {
		v    any
		want string
	}{
		{nil, ""}, {d, "2026-11-19T14:30:00Z"}, {3.0, "3"}, {0.1, "0.1"}, {true, "true"},
		{map[string]any{"a": 1}, `{"a":1}`}, {2 * time.Second, "2s"},
	} {
		if got := Format(tt.v); got != tt.want {
			t.Errorf("Format(%#v) = %q, want %q", tt.v, got, tt.want)
		}
	}
}

func TestDateFunctions(t *testing.T) {
	env := map[string]any{"params": map[string]any{"DAY": "2026-11-19", "N": float64(2)}}
	for _, tt := range []struct{ src, want string }{
		{"{{ formatDate(parseDate('19.11.2026', 'dd.MM.yyyy'), 'yyyy-MM-dd') }}", "2026-11-19"},
		{"{{ formatDate(addDays(params.DAY, -1), 'yyyyMMdd') }}", "20261118"},
		{"{{ formatDate(addDays(params.DAY, params.N), 'd.M.yy') }}", "21.11.26"},
		{"{{ formatDate(addMonths(params.DAY, 3), 'MMM yyyy') }}", "Feb 2027"},
		{"{{ formatDate(addHours(parseDate('2026-11-19 22:00', 'yyyy-MM-dd HH:mm'), 3), 'EEE HH:mm') }}", "Fri 01:00"},
		{"{{ formatDate(params.DAY, 'EEEE, dd. MMMM') }}", "Thursday, 19. November"},
		{"{{ formatDate(params.DAY, \"dd.MM.yyyy 'um' HH:mm\") }}", "19.11.2026 um 00:00"},
		{"{{ dayOfWeek(params.DAY) }}", "4"},
		{"{{ dayOfWeek('2026-11-22') }}", "7"},
		{"{{ diffDays('2026-11-19', '2026-12-01') }}", "12"},
		{"{{ diffDays('2026-12-01', '2026-11-19') }}", "-12"},
		{"{{ formatDate(today(), 'HH:mm:ss') }}", "00:00:00"},
	} {
		if got := Format(eval(t, tt.src, env)); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.src, got, tt.want)
		}
	}
	if now := eval(t, "{{ now() }}", nil).(time.Time); time.Since(now) > time.Minute {
		t.Errorf("now() = %v", now)
	}
}

func TestDateErrors(t *testing.T) {
	for _, tt := range []struct{ src, want string }{
		{"{{ formatDate(today(), 'yyyy-QQ') }}", `unknown letter 'Q'`},
		{"{{ formatDate(today(), 'yyyy 1') }}", "digits must be in 'quotes'"},
		{`{{ formatDate(today(), "yyyy 'x") }}`, "missing closing quote"},
		{"{{ parseDate('2026/11/19', 'yyyy-MM-dd') }}", `does not match the pattern "yyyy-MM-dd"`},
		{"{{ parseDate('x Jan', \"'x Jan'\") }}", "cannot be read back"},
		{"{{ addDays('tomorrow', 1) }}", `"tomorrow" is not a date`},
		{"{{ addDays(today(), 1.5) }}", "expected a whole number, got 1.5"},
	} {
		tpl, err := Compile(tt.src, WorkflowRoots)
		if err == nil {
			_, err = tpl.Eval(nil)
		}
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v, want containing %q", tt.src, err, tt.want)
		}
	}
}

func TestTextAndNullFunctions(t *testing.T) {
	env := map[string]any{"params": map[string]any{"EMPTY": "  ", "NAME": "Jürgen  Groß"}}
	for _, tt := range []struct {
		src  string
		want any
	}{
		{"{{ padLeft('7', 3, '0') }}", "007"},
		{"{{ padLeft('7', 4, 'ab') }}", "aba7"},
		{"{{ padRight('ab', 4) }}", "ab  "},
		{"{{ padLeft('abcd', 2) }}", "abcd"},
		{"{{ substr('Grüße', 1, 3) }}", "rüß"},
		{"{{ substr('Grüße', 3) }}", "ße"},
		{"{{ substr('abc', 5, 2) }}", ""},
		{"{{ left('Grüße', 2) }}", "Gr"},
		{"{{ right('Grüße', 2) }}", "ße"},
		{"{{ right('ab', 5) }}", "ab"},
		{"{{ normalize(params.NAME) }}", "Juergen Gross"},
		{"{{ coalesce(params.MISSING, params.EMPTY, 'x', 'y') }}", "x"},
		{"{{ coalesce(params.MISSING) }}", nil},
		{"{{ isEmpty(params.EMPTY) }}", true},
		{"{{ isEmpty([]) }}", true},
		{"{{ isEmpty(0) }}", false},
		{"{{ isEmpty(params.NAME) }}", false},
	} {
		if got := eval(t, tt.src, env); got != tt.want {
			t.Errorf("%s = %#v, want %#v", tt.src, got, tt.want)
		}
	}
}
