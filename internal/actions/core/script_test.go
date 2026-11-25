package core

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/actions/actiontest"
	"github.com/runnelhq/runnel/internal/paths"
)

func TestScriptConfig(t *testing.T) {
	actiontest.ConfigError(t, scriptDefinition, nil, "give the code in script or its file in file")
	actiontest.ConfigError(t, scriptDefinition, map[string]any{"script": "return 1", "file": "scripts/a.js"}, "either script or file")
	actiontest.ConfigError(t, scriptDefinition, map[string]any{"file": "scripts/a.ps1"}, "expected a .js file")
	actiontest.ConfigError(t, scriptDefinition, map[string]any{"script": "return 1", "timeout": "1s"}, "timeout")
}

// The code gets params and its return value is the output; console.* goes to the log.
func TestScript(t *testing.T) {
	a := actiontest.New(t, scriptDefinition, map[string]any{
		"params": map[string]any{
			"orders": []any{map[string]any{"amount": decimal.RequireFromString("12.50")}, map[string]any{"amount": int64(10)}},
			"rate":   0.5,
		},
		// ${…} and {{ … }} are part of the code (a template literal), not references.
		"script": "const sum = params.orders.reduce((s, o) => s + Number(o.amount), 0);\n" +
			"console.log(`${params.orders.length} orders, first ${params.orders[0].amount}`);\n" +
			"console.warn('{{ not an expression }}');\n" +
			"return { net: sum, gross: sum * (1 + params.rate), ok: true };\n",
	})
	res, logs, err := actiontest.Execute(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]any{"net": 22.5, "gross": 33.75, "ok": true}; !reflect.DeepEqual(res.Output, want) {
		t.Errorf("output %#v", res.Output)
	}
	want := []actiontest.Logged{{Level: slog.LevelInfo, Msg: "2 orders, first 12.50"}, {Level: slog.LevelWarn, Msg: "{{ not an expression }}"}}
	if !reflect.DeepEqual(logs, want) {
		t.Errorf("logs %v", logs)
	}

	// Without return, the output is empty.
	a = actiontest.New(t, scriptDefinition, map[string]any{"script": "const x = params.missing;"})
	if res, _, err := actiontest.Execute(context.Background(), a); err != nil || res.Output != nil {
		t.Errorf("no return: %#v %v", res.Output, err)
	}
}

// An exception fails the action with its message and line.
func TestScriptError(t *testing.T) {
	a := actiontest.New(t, scriptDefinition, map[string]any{"script": "if (!params.orders) {\n  throw new Error('no orders');\n}"})
	if _, _, err := actiontest.Execute(context.Background(), a); err == nil || err.Error() != "script, line 2: no orders" {
		t.Errorf("%v", err)
	}
	a = actiontest.New(t, scriptDefinition, map[string]any{"script": "return 'x'.repeat(17 * 1024 * 1024)"})
	if _, _, err := actiontest.Execute(context.Background(), a); err == nil || !strings.Contains(err.Error(), "larger than 16 MiB") {
		t.Errorf("%v", err)
	}
}

// A file relative to the project folder, with modules from scripts/.
func TestScriptFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("scripts/util.js", "exports.check = n => n % 7;\n")
	write("scripts/total.js", "const { check } = require('./scripts/util.js');\nreturn check(params.n);\n")
	write("scripts/fail.js", "\nthrow new Error('broken');\n")
	ctx := paths.WithBase(context.Background(), dir)

	a := actiontest.New(t, scriptDefinition, map[string]any{"file": "scripts/total.js", "params": map[string]any{"n": int64(100)}})
	if res, _, err := actiontest.Execute(ctx, a); err != nil || res.Output != int64(2) {
		t.Errorf("file: %#v %v", res.Output, err)
	}
	a = actiontest.New(t, scriptDefinition, map[string]any{"file": "scripts/fail.js"})
	if _, _, err := actiontest.Execute(ctx, a); err == nil || err.Error() != "scripts/fail.js, line 2: broken" {
		t.Errorf("fail: %v", err)
	}
	a = actiontest.New(t, scriptDefinition, map[string]any{"file": "scripts/missing.js"})
	if _, _, err := actiontest.Execute(ctx, a); err == nil || err.Error() != "script file scripts/missing.js not found" {
		t.Errorf("missing: %v", err)
	}
	a = actiontest.New(t, scriptDefinition, map[string]any{"script": "return require('../outside.js')"})
	if _, _, err := actiontest.Execute(ctx, a); err == nil || !strings.Contains(err.Error(), "only .js files in the folder scripts/") {
		t.Errorf("outside: %v", err)
	}
}

// The timeout of the action and cancelling the run end a script that does not end.
func TestScriptCancelled(t *testing.T) {
	a := actiontest.New(t, scriptDefinition, map[string]any{"script": "while (true) {}"})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := actiontest.Execute(ctx, a); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("timeout: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	if _, _, err := actiontest.Execute(ctx, a); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel: %v", err)
	}
}

// BenchmarkScriptAction runs a small script as one execution of the action: new
// runtime, compile, call and result.
func BenchmarkScriptAction(b *testing.B) {
	with := map[string]any{
		"params": map[string]any{"orders": []any{
			map[string]any{"amount": decimal.RequireFromString("12.50")},
			map[string]any{"amount": decimal.RequireFromString("7.25")},
		}, "rate": 0.19},
		"script": "const sum = params.orders.reduce((s, o) => s + Number(o.amount), 0);\nreturn { net: sum, gross: sum * (1 + params.rate) };",
	}
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		a, err := scriptDefinition.New(with)
		if err != nil {
			b.Fatal(err)
		}
		if _, _, err := actiontest.Execute(ctx, a); err != nil {
			b.Fatal(err)
		}
	}
}
