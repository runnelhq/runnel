package expr

import (
	"fmt"
	"strings"
	"testing"
)

// replaceRegex (S69): RE2 patterns, groups in the replacement, Unicode, flags.
func TestReplaceRegex(t *testing.T) {
	for _, tt := range []struct{ text, pattern, repl, want string }{
		{"0221 / 123-45", `[^0-9]`, "", "022112345"},
		{"19.11.2026", `^(\d{2})\.(\d{2})\.(\d{4})$`, "$3-$2-$1", "2026-11-19"},
		{"Müller, Anna", `^(?P<last>[^,]+),\s*(?P<first>.+)$`, "${first} ${last}", "Anna Müller"},
		{"a  b \t c", `\s+`, " ", "a b c"},
		{"Order-17", `(\d+)`, "${1}x", "Order-17x"},
		{"Order-17", `(\d+)`, "$1x", "Order-"}, // $1x is the group "1x", which does not exist
		{"10 EUR", `EUR`, "$$", "10 $"},
		{"ÄÖÜ äöü 123", `\p{L}+`, "*", "* * 123"},
		{"Köln KÖLN köln", `(?i)köln`, "Cologne", "Cologne Cologne Cologne"},
		{"abc", `x*`, "-", "-a-b-c-"},
		{"", `.*`, "empty", "empty"},
	} {
		env := map[string]any{"params": map[string]any{"T": tt.text, "P": tt.pattern, "R": tt.repl}}
		if got := eval(t, "{{ replaceRegex(params.T, params.P, params.R) }}", env); got != tt.want {
			t.Errorf("replaceRegex(%q, %q, %q) = %q, want %q", tt.text, tt.pattern, tt.repl, got, tt.want)
		}
	}
	// A pattern with \ is written in backticks: in '…' \d would be an escape of the string.
	if got := eval(t, "{{ replaceRegex('19.11.2026', `^(\\d{2})\\.(\\d{2})\\.(\\d{4})$`, '$3-$2-$1') }}", nil); got != "2026-11-19" {
		t.Errorf("backticks: %#v", got)
	}
	if _, err := Compile(`{{ replaceRegex('x', '\d', '') }}`, WorkflowRoots); err == nil || !strings.Contains(err.Error(), "invalid char escape") {
		t.Errorf("\\d in quotes: %v", err)
	}

	// A number from a record or parameter is written as text first; the result is text.
	if got := eval(t, "{{ replaceRegex(params.N, '3', '-') }}", map[string]any{"params": map[string]any{"N": int64(12345)}}); got != "12-45" {
		t.Errorf("number: %#v", got)
	}

	// Errors name the pattern; wrong arguments are found when compiling.
	for pattern, want := range map[string]string{
		`[a`:                      `replaceRegex: invalid pattern "[a": missing closing ]`,
		`a(b`:                     `invalid pattern "a(b": missing closing )`,
		`(?<=a)b`:                 `invalid pattern "(?<=a)b": lookahead and lookbehind like (?<=…) are not supported`,
		strings.Repeat("a", 4097): "replaceRegex: the pattern is longer than 4096 characters",
		`a{2000}`:                 `invalid repeat count`,
	} {
		tpl, err := Compile("{{ replaceRegex('x', params.P, '') }}", WorkflowRoots)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tpl.Eval(map[string]any{"params": map[string]any{"P": pattern}})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%.20q: %v, want %q", pattern, err, want)
		}
	}
	if _, err := Compile("{{ replaceRegex('x', '[0-9]') }}", WorkflowRoots); err == nil {
		t.Error("two arguments accepted")
	}
}

// The cache keeps at most maxPatterns compiled patterns.
func TestPatternCache(t *testing.T) {
	for i := range maxPatterns + 10 {
		if _, err := compilePattern(fmt.Sprintf("a{%d}", i%1000)); err != nil {
			t.Fatal(err)
		}
	}
	patterns.Lock()
	n := len(patterns.m)
	patterns.Unlock()
	if n == 0 || n > maxPatterns {
		t.Errorf("%d patterns kept", n)
	}
}

// BenchmarkReplaceRegex evaluates replaceRegex once per record, as core.calculate does:
// the pattern is compiled once and taken from the cache.
func BenchmarkReplaceRegex(b *testing.B) {
	tpl, err := Compile("{{ replaceRegex(params.T, '[^0-9]', '') }}", WorkflowRoots)
	if err != nil {
		b.Fatal(err)
	}
	env := map[string]any{"params": map[string]any{"T": "+49 (0)221 / 123-456"}}
	b.ReportAllocs()
	for b.Loop() {
		if v, err := tpl.Eval(env); err != nil || v != "490221123456" {
			b.Fatal(v, err)
		}
	}
}
