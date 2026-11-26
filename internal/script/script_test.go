package script

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
)

type line struct {
	level slog.Level
	msg   string
}

// run compiles body with the parameter params and calls it with p.
func run(t *testing.T, opts Options, body string, p any) (any, error) {
	t.Helper()
	r := New(opts)
	fn, err := r.Compile("script", body, "params")
	if err != nil {
		return nil, err
	}
	return r.Call(context.Background(), fn, p)
}

// Values go into the script as described in ADR 0028 (decimal and date as text, large
// integers as BigInt) and come back as Go values.
func TestValues(t *testing.T) {
	when := time.Date(2026, 11, 25, 8, 30, 0, 0, time.UTC)
	in := map[string]any{
		"text": "a", "int": int64(42), "float": 1.5, "bool": true, "none": nil,
		"dec": decimal.RequireFromString("12.50"), "date": record.Date{Year: 2026, Month: 11, Day: 25},
		"time": when, "big": int64(1) << 60, "bytes": []byte{1, 2, 3}, "small": int32(7),
		"list": []any{"x", int64(1)}, "rows": []map[string]any{{"id": int64(1)}},
		"nested": map[string]any{"a": map[string]any{"b": []any{true}}},
	}
	body := `return {
		types: Object.fromEntries(Object.entries(params).map(([k, v]) => [k, v === null ? "null" : v instanceof Date ? "date" : v instanceof Uint8Array ? "bytes" : Array.isArray(v) ? "array" : typeof v])),
		dec: params.dec, date: params.date, time: params.time.toISOString(), big: String(params.big),
		bytes: params.bytes.length, nested: params.nested.a.b[0], rows: params.rows[0].id,
		int: params.int + 1, half: params.int / 4, whole: 6 / 2, nan: NaN, inf: Infinity,
		undef: undefined, bigint: 5n, when: new Date(Date.UTC(2026, 0, 2)), boxed: new String("s"),
		method() { return 1 }, list: [1, "two", null, [false]],
	};`
	got, err := run(t, Options{}, body, in)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"types": map[string]any{"text": "string", "int": "number", "float": "number", "bool": "boolean", "none": "null",
			"dec": "string", "date": "string", "time": "date", "big": "bigint", "bytes": "bytes", "small": "number",
			"list": "array", "rows": "array", "nested": "object"},
		"dec": "12.50", "date": "2026-11-25", "time": "2026-11-25T08:30:00.000Z", "big": "1152921504606846976",
		"bytes": int64(3), "nested": true, "rows": int64(1),
		"int": int64(43), "half": 10.5, "whole": int64(3), "nan": nil, "inf": nil,
		"undef": nil, "bigint": int64(5), "boxed": "s",
		"list": []any{int64(1), "two", nil, []any{false}},
	}
	m := got.(map[string]any)
	if w, ok := m["when"].(time.Time); !ok || !w.Equal(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("when = %#v", m["when"])
	}
	delete(m, "when")
	if !reflect.DeepEqual(m, want) {
		t.Errorf("got  %#v\nwant %#v", m, want)
	}

	// Without return, the result is empty; a plain value comes back as it is.
	for body, want := range map[string]any{"let x = 1;": nil, "return 'x'": "x", "return [params]": []any{int64(1)}} {
		if got, err := run(t, Options{}, body, int64(1)); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %#v %v", body, got, err)
		}
	}
}

// Results that cannot be data are errors.
func TestResultErrors(t *testing.T) {
	for body, want := range map[string]string{
		"return () => 1":                             "a function cannot be part of the result",
		"const a = {}; a.self = a; return a":         "nested deeper than 100 levels",
		"return 2n ** 70n":                           "too large for an integer",
		"return Symbol('x')":                         "cannot be part of the result",
		"return 'x'.repeat(17 * 1024 * 1024)":        "larger than 16 MiB",
		"return Array(20000).fill('y'.repeat(1000))": "larger than 16 MiB",
		"return {a: 'x'.repeat(9 * 1024 * 1024), b: 'x'.repeat(9 * 1024 * 1024)}": "larger than 16 MiB",
	} {
		if _, err := run(t, Options{}, body, nil); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", body, err, want)
		}
	}
}

// An exception names its message and the line of the script; a syntax error too.
func TestExceptions(t *testing.T) {
	for body, want := range map[string]string{
		"\n\nthrow new Error('no orders')":                  "script, line 3: no orders",
		"const x = null;\nreturn x.name":                    "script, line 2: TypeError: Cannot read property 'name'",
		"throw 'plain'":                                     "script, line 1: plain",
		"class Bad extends Error {}\nthrow new Bad('mine')": "script, line 2: mine",
		"\nreturn (": "script, line 3: syntax error",
		"function f() { return f() }\nreturn f()": "script: too much recursion",
	} {
		if _, err := run(t, Options{}, body, nil); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", body, err, want)
		}
	}
}

// console.* writes to the log with its level; lines and their length are limited.
func TestConsole(t *testing.T) {
	var lines []line
	opts := Options{Log: func(l slog.Level, m string) { lines = append(lines, line{l, m}) }}
	if _, err := run(t, opts, `console.log("a", 1, {b: [2]}, null, undefined); console.info("i"); console.warn("w"); console.error(new Error("e"));`, nil); err != nil {
		t.Fatal(err)
	}
	want := []line{{slog.LevelInfo, `a 1 {"b":[2]} null undefined`}, {slog.LevelInfo, "i"}, {slog.LevelWarn, "w"}, {slog.LevelError, "Error: e"}}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("got %v", lines)
	}

	lines = nil
	if _, err := run(t, opts, `for (let i = 0; i < 1500; i++) console.log(i); console.log("ä".repeat(5000))`, nil); err != nil {
		t.Fatal(err)
	}
	if len(lines) != MaxLines+1 || lines[MaxLines-1].msg != "999" || lines[MaxLines].level != slog.LevelWarn ||
		!strings.Contains(lines[MaxLines].msg, "more than 1000 lines") {
		t.Errorf("%d lines, last %v", len(lines), lines[len(lines)-1])
	}

	lines = nil
	if _, err := run(t, opts, `console.log("ä".repeat(5000))`, nil); err != nil {
		t.Fatal(err)
	}
	if l := lines[0].msg; len(l) > MaxLineBytes || !strings.HasSuffix(l, "ä…") {
		t.Errorf("long line: %d bytes, ends %q", len(l), l[len(l)-8:])
	}

	// The count starts anew with each call.
	r := New(opts)
	fn, _ := r.Compile("script", "for (let i = 0; i < 1000; i++) console.log(i)", "params")
	lines = nil
	for range 2 {
		if _, err := r.Call(context.Background(), fn, nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(lines) != 2*MaxLines {
		t.Errorf("%d lines in two calls", len(lines))
	}
}

// A timeout or cancel interrupts a script that does not end, even one that catches
// every exception; the runtime can be used again afterwards.
func TestInterrupt(t *testing.T) {
	r := New(Options{})
	fn, err := r.Compile("script", "while (true) { try { while (true) {} } catch (e) {} }", "params")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := r.Call(ctx, fn, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("timeout: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %s", d)
	}

	ctx, cancel = context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	if _, err := r.Call(ctx, fn, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel: %v", err)
	}
	if _, err := r.Call(ctx, fn, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled before: %v", err)
	}

	ok, _ := r.Compile("script", "return 1", "params")
	if v, err := r.Call(context.Background(), ok, nil); err != nil || v != int64(1) {
		t.Errorf("after interrupt: %v %v", v, err)
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// require loads .js files from scripts/ of the project, each once per runtime.
func TestRequire(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "scripts", "util.js"), "\uFEFFconsole.log('loading util');\nexports.double = x => x * 2;\n")
	writeFile(t, filepath.Join(dir, "scripts", "lib", "tax.js"), "const u = require('./scripts/util.js');\nmodule.exports = { gross: n => u.double(n) + 1 };\n")
	writeFile(t, filepath.Join(dir, "scripts", "bad.js"), "\n\nthrow new Error('broken module');\n")
	writeFile(t, filepath.Join(dir, "scripts", "syntax.js"), "exports.x = (;\n")
	writeFile(t, filepath.Join(dir, "scripts", "data.txt"), "exports.x = 1;\n")
	writeFile(t, filepath.Join(dir, "other", "x.js"), "exports.x = 1;\n")
	writeFile(t, filepath.Join(dir, "x.js"), "exports.x = 1;\n")

	var lines []string
	opts := Options{Dir: dir, Log: func(_ slog.Level, m string) { lines = append(lines, m) }}
	got, err := run(t, opts, `
		const a = require('./scripts/util.js');
		const b = require('scripts/util.js');
		const tax = require('./scripts/lib/tax.js');
		return [a.double(params), a === b, tax.gross(params)];`, int64(4))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []any{int64(8), true, int64(9)}) || !reflect.DeepEqual(lines, []string{"loading util"}) {
		t.Errorf("got %v, log %v", got, lines)
	}

	abs := filepath.Join(dir, "scripts", "util.js")
	for name, want := range map[string]string{
		"./other/x.js":               "only .js files in the folder scripts/",
		"./x.js":                     "only .js files in the folder scripts/",
		"./scripts/../x.js":          "only .js files in the folder scripts/",
		"scripts/../scripts/util.js": "only .js files in the folder scripts/",
		`scripts\..\x.js`:            "only .js files in the folder scripts/",
		abs:                          "only .js files in the folder scripts/",
		"/scripts/util.js":           "only .js files in the folder scripts/",
		"./scripts/data.txt":         "only .js files in the folder scripts/",
		"./scripts":                  "only .js files in the folder scripts/",
		"lodash":                     "only .js files in the folder scripts/",
		"./scripts/missing.js":       "file not found",
		"./scripts/bad.js":           "scripts/bad.js, line 3: broken module",
		"./scripts/syntax.js":        "scripts/syntax.js, line 1: syntax error",
	} {
		_, err := run(t, opts, fmt.Sprintf("return require(%q)", name), nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("require(%q): %v, want %q", name, err, want)
		}
	}

	// The script can catch a failed require.
	if got, err := run(t, opts, "try { require('./x.js') } catch (e) { return e.message }", nil); err != nil || !strings.Contains(got.(string), "only .js files") {
		t.Errorf("catch: %v %v", got, err)
	}
	// Without a project folder there is no require.
	if _, err := run(t, Options{}, "return require('./scripts/util.js')", nil); err == nil || !strings.Contains(err.Error(), "without a project folder") {
		t.Errorf("no dir: %v", err)
	}
}

// A link in scripts/ that leads out of it is refused.
func TestRequireSymlink(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "secret.js"), "exports.x = 1;\n")
	writeFile(t, filepath.Join(dir, "scripts", "ok.js"), "exports.x = 1;\n")
	if err := os.Symlink(filepath.Join(dir, "secret.js"), filepath.Join(dir, "scripts", "link.js")); err != nil {
		t.Skipf("no symbolic links: %v", err)
	}
	if _, err := run(t, Options{Dir: dir}, "return require('./scripts/link.js')", nil); err == nil || !strings.Contains(err.Error(), "outside the folder scripts/") {
		t.Errorf("link: %v", err)
	}
}

// A require that does not end is interrupted, too.
func TestRequireInterrupt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "scripts", "loop.js"), "while (true) {}\n")
	r := New(Options{Dir: dir})
	fn, _ := r.Compile("script", "try { require('./scripts/loop.js') } catch (e) {} while (true) {}", "params")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := r.Call(ctx, fn, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("%v", err)
	}
}

func TestCut(t *testing.T) {
	if got := cut("abc", 3); got != "abc" {
		t.Error(got)
	}
	if got := cut(strings.Repeat("ä", 10), 8); got != "ää…" {
		t.Errorf("%q", got)
	}
}

func BenchmarkCall(b *testing.B) {
	params := map[string]any{"orders": []any{
		map[string]any{"amount": decimal.RequireFromString("12.50")},
		map[string]any{"amount": decimal.RequireFromString("7.25")},
	}, "rate": 0.19}
	b.ReportAllocs()
	for b.Loop() {
		r := New(Options{})
		fn, err := r.Compile("script", "const sum = params.orders.reduce((s, o) => s + Number(o.amount), 0);\nreturn { net: sum, gross: sum * (1 + params.rate) };", "params")
		if err != nil {
			b.Fatal(err)
		}
		if _, err := r.Call(context.Background(), fn, params); err != nil {
			b.Fatal(err)
		}
	}
}
