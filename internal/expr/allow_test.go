package expr

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/expr-lang/expr/builtin"
)

// TestUndocumentedFunctionsRefused: only the functions of the reference can be called
// (R5a); the other built-ins of expr-lang are refused with similar documented ones.
func TestUndocumentedFunctionsRefused(t *testing.T) {
	for _, tt := range []struct{ src, want string }{
		{"{{ date('2026-01-01') }}", `unknown function "date" (similar: `},
		{"{{ duration('1h') }}", `unknown function "duration"`},
		{"{{ type(1) }}", `unknown function "type"`},
		{"{{ repeat('a', 2) }}", `unknown function "repeat"`},
		{"{{ toBase64('a') }}", `unknown function "toBase64"`},
		{"{{ trimPrefix('ab', 'a') }}", `unknown function "trimPrefix" (similar: trim`},
		{"{{ upper(timezone('UTC')) }}", `unknown function "timezone"`},
		{"{{ [1, 2] | uniq() }}", `unknown function "uniq"`},
		{"{{ _add(1, 2) }}", `unknown function "_add"`},
		{"{{ uper('a') }}", `unknown function "uper" (similar: upper`},
		{"{{ frobnicate() }}", `unknown function "frobnicate" (the functions are listed by "runnel catalog --expressions")`},
	} {
		_, err := Compile(tt.src, WorkflowRoots)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error %v, want containing %q", tt.src, err, tt.want)
		}
	}
	if _, err := Compile("{{ date('2026-01-01') }}", WorkflowRoots); err == nil || !strings.Contains(err.Error(), "parseDate") || !strings.Contains(err.Error(), "formatDate") {
		t.Errorf("date: %v, want parseDate and formatDate as similar", err)
	}
	// The check also applies to ${…} placeholders checked before they are resolved.
	if err := Check("{{ date('${DAY}') }}", WorkflowRoots); err == nil {
		t.Error("Check: date accepted")
	}
}

// TestBuiltinsDocumentedOrRefused: every built-in of the pinned expr-lang is either in
// the reference or refused, so an update of expr-lang cannot add functions unnoticed.
func TestBuiltinsDocumentedOrRefused(t *testing.T) {
	for _, name := range builtin.Names {
		src := fmt.Sprintf("{{ %s(1) }}", name)
		_, err := Compile(src, WorkflowRoots)
		refused := err != nil && strings.Contains(err.Error(), "unknown function")
		documented := slices.ContainsFunc(Functions, func(d Doc) bool { return d.Name == name })
		if documented == refused {
			t.Errorf("%s: documented %v, refused %v (%v)", name, documented, refused, err)
		}
	}
}

func TestDocumentedBuiltins(t *testing.T) {
	env := map[string]any{"params": map[string]any{"N": "7", "LIST": []any{map[string]any{"n": 2}, map[string]any{"n": 1}}}}
	for _, tt := range []struct {
		src  string
		want any
	}{
		{"{{ int('42') + 1 }}", 43},
		{"{{ int(params.N) * 2 }}", 14},
		{"{{ float('1.5') * 2 }}", 3.0},
		{"{{ string(42) + ' orders' }}", "42 orders"},
		{"{{ abs(-3) }}", 3},
		{"{{ ceil(2.1) }}", 3.0},
		{"{{ floor(2.9) }}", 2.0},
		{"{{ min(3, 1, 2) }}", 1},
		{"{{ max([3, 1, 2]) }}", 3},
		{"{{ hasPrefix('INV-4711', 'INV-') }}", true},
		{"{{ hasSuffix('orders.csv', '.txt') }}", false},
		{"{{ fromJSON('{\"id\": 7}').id }}", 7.0},
		{"{{ toJSON([1]) }}", "[\n  1\n]"},
		{"{{ first(['a', 'b']) + last(['a', 'b']) }}", "ab"},
		{"{{ len(keys({'a': 1})) }}", 1},
		{"{{ count([1, 5, 9], # > 3) }}", 2},
		{"{{ all([1, 5], # > 0) && none([1, 5], # > 9) && any([1, 5], # > 3) && one([1, 5], # > 3) }}", true},
		{"{{ find([1, 5, 9], # > 3) }}", 5},
		{"{{ findIndex([1, 5, 9], # > 3) }}", 1},
		{"{{ findLastIndex([1, 5, 9], # > 3) }}", 2},
		{"{{ findLast([1, 5, 9], # > 3) }}", 9},
		{"{{ sum(params.LIST, .n) }}", 3},
		{"{{ sortBy(params.LIST, .n)[0].n }}", 1},
		{"{{ len(groupBy(params.LIST, .n)) }}", 2},
		{"{{ reduce([1, 2, 3], #acc + #, 0) }}", 6},
		{"{{ [1, 5, 9] | filter(# > 3) | len() }}", 2},
	} {
		if got := eval(t, tt.src, env); fmt.Sprint(got) != fmt.Sprint(tt.want) {
			t.Errorf("%s = %#v, want %#v", tt.src, got, tt.want)
		}
	}
}
