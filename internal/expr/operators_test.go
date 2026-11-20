package expr

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func recordEnv(fields map[string]any) map[string]any {
	return map[string]any{"record": fields, "vars": map[string]any{}, "params": map[string]any{"LIMIT": 100}, "env": map[string]any{}, "run": map[string]any{}}
}

func evalRecord(src string, fields map[string]any) (any, error) {
	tpl, err := Compile(src, RecordRoots)
	if err != nil {
		return nil, err
	}
	return tpl.Eval(recordEnv(fields))
}

func TestDecimalOperators(t *testing.T) {
	d := decimal.RequireFromString
	fields := map[string]any{"amount": d("1234.50"), "cent": d("0.1"), "qty": int64(3), "price": 2.5, "none": nil,
		"name": "Ada", "day": time.Date(2026, 11, 19, 0, 0, 0, 0, time.Local)}
	for _, tt := range []struct {
		src  string
		want any
	}{
		// Exact: 0.1 + 0.2 is 0.3, not 0.30000000000000004.
		{"{{ record.cent + 0.2 }}", d("0.3")},
		{"{{ record.amount * 1.19 }}", d("1469.055")},
		{"{{ record.amount * record.qty }}", d("3703.5")},
		{"{{ record.amount - 34.5 }}", d("1200")},
		{"{{ record.amount / 4 }}", d("308.625")},
		{"{{ record.amount % 1000 }}", d("234.5")},
		{"{{ -record.amount }}", d("-1234.5")},
		{"{{ record.amount > 1000 }}", true},
		{"{{ record.amount >= 1234.5 }}", true},
		{"{{ record.amount < params.LIMIT }}", false},
		{"{{ record.amount <= record.qty }}", false},
		{"{{ record.amount == 1234.5 }}", true},
		{"{{ record.amount != 1234.5 }}", false},
		{"{{ record.amount == '1234.5' }}", false},
		{"{{ record.amount > 0 && record.name == 'Ada' }}", true},
		// Other types behave as before.
		{"{{ record.qty * 2 }}", 6},
		{"{{ record.qty + record.price }}", 5.5},
		{"{{ record.qty / 2 }}", 1.5},
		{"{{ -record.qty }}", int64(-3)},
		{"{{ record.name + '!' }}", "Ada!"},
		{"{{ record.day < now() }}", true},
		{"{{ record.day == today() }}", false},
		// A missing value is neither smaller nor larger; equality with nil works.
		{"{{ record.none > 0 }}", false},
		{"{{ record.none < 0 }}", false},
		{"{{ record.none == nil }}", true},
		{"{{ record.missing != nil }}", false},
		{"{{ record.amount > record.none }}", false},
		// Arithmetic with null is null (as in SQL).
		{"{{ record.none + 1 }}", nil},
		{"{{ record.none * record.amount > 0 }}", false},
		{"{{ -record.none }}", nil},
	} {
		got, err := evalRecord(tt.src, fields)
		if err != nil {
			t.Errorf("%s: %v", tt.src, err)
			continue
		}
		if wd, ok := tt.want.(decimal.Decimal); ok {
			if gd, ok := got.(decimal.Decimal); !ok || !gd.Equal(wd) {
				t.Errorf("%s = %v (%T), want decimal %v", tt.src, got, got, wd)
			}
			continue
		}
		if got != tt.want {
			t.Errorf("%s = %v (%T), want %v (%T)", tt.src, got, got, tt.want, tt.want)
		}
	}
}

func TestOperatorErrors(t *testing.T) {
	fields := map[string]any{"amount": decimal.RequireFromString("5"), "text": "5", "zero": decimal.Zero, "none": nil}
	for _, tt := range []struct{ src, want string }{
		{"{{ record.amount + record.text }}", "invalid operation: decimal + string"},
		{"{{ record.amount > record.text }}", "invalid operation: decimal > string"},
		{"{{ record.text > 3 }}", "invalid operation"},
		{"{{ record.amount / record.zero }}", "division by zero"},
	} {
		_, err := evalRecord(tt.src, fields)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error %v, want %q", tt.src, err, tt.want)
		}
	}
	// Unknown names are still found when compiling.
	if _, err := Compile("{{ recrd.amount > 0 }}", RecordRoots); err == nil {
		t.Error("unknown name accepted")
	}
}

func TestFormatDecimal(t *testing.T) {
	if got := Format(decimal.RequireFromString("1234.50")); got != "1234.5" {
		t.Errorf("Format = %q", got)
	}
}

func TestRow(t *testing.T) {
	for _, tt := range []struct {
		src  string
		row  any
		want any
	}{
		{"{{ row * 10 }}", int64(2), 20},
		{"{{ row.path }}", map[string]any{"path": "a.csv"}, "a.csv"},
		{"{{ upper(row) }}", "abc", "ABC"},
		{"{{ row == 'x' }}", "x", true},
	} {
		tpl, err := Compile(tt.src, RowRoots)
		if err != nil {
			t.Errorf("%s: %v", tt.src, err)
			continue
		}
		got, err := tpl.Eval(map[string]any{"row": tt.row, "params": map[string]any{}, "vars": map[string]any{}, "env": map[string]any{}, "run": map[string]any{}, "result": nil, "results": map[string]any{}})
		if err != nil || got != tt.want {
			t.Errorf("%s = %#v %v, want %#v", tt.src, got, err, tt.want)
		}
	}
	if _, err := Compile("{{ row }}", WorkflowRoots); err == nil {
		t.Error("row without forEach accepted")
	}
}
