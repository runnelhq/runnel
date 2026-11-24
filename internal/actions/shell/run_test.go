package shell

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/proc"
)

// The test binary also serves as the program: with RUNNEL_SHELL_HELPER set, TestMain does
// what the mode says instead of running the tests.
func TestMain(m *testing.M) {
	switch os.Getenv("RUNNEL_SHELL_HELPER") {
	case "":
		os.Exit(m.Run())
	case "echo": // arguments and TOKEN to stdout, a line to stderr, exit code from EXIT
		wd, _ := os.Getwd()
		fmt.Printf("%s|token=%s|dir=%s\n", strings.Join(os.Args[1:], ","), os.Getenv("TOKEN"), filepath.Base(wd))
		fmt.Fprintln(os.Stderr, "warning: something")
		var code int
		_, _ = fmt.Sscan(os.Getenv("EXIT"), &code)
		os.Exit(code)
	case "json":
		fmt.Println(`{"files": 3, "ok": true}`)
	case "sleep":
		time.Sleep(time.Minute)
	}
	os.Exit(0)
}

// run starts the test binary with shell.run in the project folder dir.
func run(t *testing.T, dir, mode string, with map[string]any) (map[string]any, []string, error) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNNEL_SHELL_HELPER", mode)
	with["program"] = self
	a, err := newRun(with)
	if err != nil {
		return nil, nil, err
	}
	var logs []string
	ec := &action.ExecContext{Log: func(_ slog.Level, m string) { logs = append(logs, m) }}
	res, err := a.Execute(paths.WithBase(context.Background(), dir), ec)
	out, _ := res.Output.(map[string]any)
	return out, logs, err
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Each argument as one; the secret from env never appears in the output; relative workdir.
	out, logs, err := run(t, dir, "echo", map[string]any{"args": []any{"a b", "c;d"}, "env": map[string]any{"TOKEN": "s3cret-value"}, "workdir": "work"})
	if err != nil {
		t.Fatal(err)
	}
	if out["exitCode"] != int64(0) || strings.TrimSpace(out["stdout"].(string)) != "a b,c;d|token=***|dir=work" || out["stderr"] != "warning: something\n" {
		t.Errorf("%#v", out)
	}
	if !strings.Contains(logs[len(logs)-1], "ended with exit code 0") {
		t.Errorf("%v", logs)
	}
	// Another exit code fails, with the last lines of stderr, unless it is in okCodes.
	_, _, err = run(t, dir, "echo", map[string]any{"env": map[string]any{"EXIT": "3"}})
	if err == nil || !strings.HasSuffix(err.Error(), "ended with exit code 3: warning: something") {
		t.Errorf("%v", err)
	}
	if out, _, err := run(t, dir, "echo", map[string]any{"env": map[string]any{"EXIT": "3"}, "okCodes": []any{0, 3}}); err != nil || out["exitCode"] != int64(3) {
		t.Errorf("okCodes: %v %v", out, err)
	}
	// json reads stdout.
	out, _, err = run(t, dir, "json", map[string]any{"json": true})
	if err != nil || out["json"].(map[string]any)["files"] != float64(3) {
		t.Errorf("json: %#v %v", out, err)
	}
	if _, _, err := run(t, dir, "echo", map[string]any{"json": true}); err == nil || !strings.Contains(err.Error(), "is not JSON") {
		t.Errorf("not json: %v", err)
	}
}

func TestRunRejects(t *testing.T) {
	dir := t.TempDir()
	start := time.Now()
	_, _, err := run(t, dir, "sleep", map[string]any{"timeout": "300ms"})
	if err == nil || !strings.Contains(err.Error(), "took longer than 300ms and was ended with the processes it started") || time.Since(start) > 10*time.Second {
		t.Errorf("timeout: %v", err)
	}
	// A batch file with a character cmd.exe interprets in an argument does not start.
	bat := filepath.Join(dir, "job.bat")
	if err := os.WriteFile(bat, []byte("@echo off\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	a, _ := newRun(map[string]any{"program": "./job.bat", "args": []any{"x & del *"}})
	if _, err := a.Execute(paths.WithBase(context.Background(), dir), &action.ExecContext{Log: func(slog.Level, string) {}}); err == nil || !strings.Contains(err.Error(), "which cmd.exe interprets") {
		t.Errorf("batch: %v", err)
	}
	for want, with := range map[string]map[string]any{
		`missing required field "program"`:    {},
		`invalid encoding "latin"`:            {"program": "x", "encoding": "latin"},
		`invalid timeout "soon"`:              {"program": "x", "timeout": "soon"},
		`maxOutput: "-1" is not a size`:       {"program": "x", "maxOutput": "-1"},
		`maxOutput: "2GB" is larger than 1GB`: {"program": "x", "maxOutput": "2GB"},
		`env: invalid name "A=B"`:             {"program": "x", "env": map[string]any{"A=B": "1"}},
	} {
		if _, err := newRun(with); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: %v, want %q", with, err, want)
		}
	}
	a, _ = newRun(map[string]any{"program": "no-such-program-runnel"})
	if _, err := a.Execute(context.Background(), &action.ExecContext{Log: func(slog.Level, string) {}}); err == nil || !strings.Contains(err.Error(), "not found in PATH") {
		t.Errorf("missing: %v", err)
	}
}

// maxOutput is a size like 1MB (ADR 0025); a number alone counts bytes, empty is 1 MiB.
func TestOutputLimit(t *testing.T) {
	for text, want := range map[string]int{"": proc.DefaultMaxOutput, "1KB": 1024, "64": 64, "2 MB": 2 << 20} {
		if got, err := outputLimit(text); err != nil || got != want {
			t.Errorf("outputLimit(%q) = %d, %v; want %d", text, got, err, want)
		}
	}
}
