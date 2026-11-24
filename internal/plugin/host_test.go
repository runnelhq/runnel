package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/plugin/plugintest"
)

// The test binary is its own plugin: started with RUNNEL_TEST_PLUGIN it speaks the
// protocol as a plugin would (without SDK, ADR 0019), with the behaviour of the mode.
func TestMain(m *testing.M) {
	plugintest.ServeIfPlugin()
	os.Exit(m.Run())
}

// plugin returns a plugin whose program is this test binary in the given mode.
func testPlugin(t *testing.T, mode string) (*model.Plugin, Options, *[]string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(plugintest.ModeEnv, mode)
	var mu sync.Mutex
	var lines []string
	o := Options{
		ProjectDir: t.TempDir(), RunnelVersion: "test",
		Log: func(level slog.Level, msg string) {
			mu.Lock()
			defer mu.Unlock()
			lines = append(lines, level.String()+" "+msg)
		},
		Mask: func(s string) string { return strings.ReplaceAll(s, "s3cret", "***") },
	}
	return &model.Plugin{Name: "demo", Namespace: "demo", Dir: t.TempDir(), Executable: exe}, o, &lines
}

// A plugin starts with initialize, describes its actions, runs them with the context of
// the run and its log on stderr; secrets are masked in everything it returns.
func TestPlugin(t *testing.T) {
	p, o, lines := testPlugin(t, "normal")
	ctx := context.Background()
	pr, err := Start(ctx, p, o)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Info.Version != "1.0.0" || pr.Info.Concurrency != 0 {
		t.Errorf("info %+v", pr.Info)
	}
	d, err := pr.Describe(ctx)
	ds := d.Actions
	if err != nil || len(ds) != 1 || ds[0].Type != "demo.echo" || ds[0].Kind != "action" || ds[0].Schema.Properties == nil {
		t.Fatalf("describe %+v %v", ds, err)
	}
	out, err := pr.Execute(ctx, "demo.echo", map[string]any{"text": "hi"}, ExecContext{RunID: "R1", ActionID: "a", ProjectDir: o.ProjectDir})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(out)
	if want := fmt.Sprintf(`{"dir":%q,"echo":"hi","list":["***"],"run":"R1","token":"***"}`, o.ProjectDir); string(got) != want {
		t.Errorf("output %s, want %s", got, want)
	}
	if _, err := pr.Execute(ctx, "demo.echo", map[string]any{"text": "fail"}, ExecContext{}); err == nil || err.Error() != "cannot echo with password ***" {
		t.Errorf("fail: %v", err)
	}
	if err := pr.Close(); err != nil {
		t.Fatal(err)
	}
	want := "WARN demo: echo hi with ***|INFO demo: plain line"
	if got := strings.Join(*lines, "|"); got != want {
		t.Errorf("log %q, want %q", got, want)
	}
}

// Without concurrency the host sends one request at a time; with it, several.
func TestConcurrency(t *testing.T) {
	for _, tt := range []struct {
		mode   string
		serial bool
	}{{"normal", true}, {"parallel", false}} {
		p, o, _ := testPlugin(t, tt.mode)
		pr, err := Start(context.Background(), p, o)
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		var wg sync.WaitGroup
		for range 3 {
			wg.Go(func() {
				if _, err := pr.Execute(context.Background(), "demo.echo", map[string]any{"text": "sleep"}, ExecContext{}); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		took := time.Since(start)
		if serial := took >= 850*time.Millisecond; serial != tt.serial {
			t.Errorf("%s: 3 requests took %v", tt.mode, took)
		}
		_ = pr.Close()
	}
}

// A plugin that speaks another version, answers with another name, has no program for
// this system or describes types outside its namespace is not used.
func TestPluginRefused(t *testing.T) {
	for mode, want := range map[string]string{
		"v2":         "protocol version 2 is not supported (Runnel speaks 1)",
		"other-name": `the program answers as plugin "other", not "demo"`,
	} {
		p, o, _ := testPlugin(t, mode)
		if _, err := Start(context.Background(), p, o); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", mode, err)
		}
	}
	p, o, _ := testPlugin(t, "outside")
	pr, err := Start(context.Background(), p, o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pr.Describe(context.Background()); err == nil || !strings.Contains(err.Error(), `type core.echo is outside its namespace "demo"`) {
		t.Errorf("outside: %v", err)
	}
	_ = pr.Close()
	p, o, _ = testPlugin(t, "conn-outside")
	if pr, err = Start(context.Background(), p, o); err != nil {
		t.Fatal(err)
	}
	if _, err := pr.Describe(context.Background()); err == nil || !strings.Contains(err.Error(), `connection type "sql" is outside its namespace (expected demo or demo-…)`) {
		t.Errorf("connection outside: %v", err)
	}
	_ = pr.Close()

	other := "linux"
	if runtime.GOOS == "linux" {
		other = "windows"
	}
	p.Executable = map[string]any{other: "x"}
	if _, err := Start(context.Background(), p, o); err == nil || !strings.Contains(err.Error(), "plugin demo has no executable for "+runtime.GOOS) {
		t.Errorf("no executable: %v", err)
	}
	p.Executable = "bin/missing.exe"
	if _, err := Start(context.Background(), p, o); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing program: %v", err)
	}
}

// A plugin that ends during a request fails it with its last lines of stderr; a
// cancelled request sends $/cancelRequest and returns at once.
func TestCrashAndCancel(t *testing.T) {
	p, o, _ := testPlugin(t, "normal")
	pr, err := Start(context.Background(), p, o)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := pr.Execute(ctx, "demo.echo", map[string]any{"text": "hang"}, ExecContext{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("hang: %v", err)
	}
	_, err = pr.Execute(context.Background(), "demo.echo", map[string]any{"text": "crash"}, ExecContext{})
	if err == nil || !strings.Contains(err.Error(), "the plugin ended unexpectedly; last lines: panic: boom") {
		t.Errorf("crash: %v", err)
	}
	if _, err := pr.Execute(context.Background(), "demo.echo", map[string]any{"text": "x"}, ExecContext{}); err == nil {
		t.Error("execute after the end")
	}
	_ = pr.Close()
}

// Program finds the executable in the folder of the plugin and turns arguments naming
// a file there into full paths.
func TestProgram(t *testing.T) {
	dir := t.TempDir()
	name := "plugin.exe"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &model.Plugin{Name: "demo", Dir: dir, Executable: name, Args: []string{"main.py", "--quiet"}}
	program, args, err := Program(p)
	if err != nil {
		t.Fatal(err)
	}
	if program != filepath.Join(dir, name) || args[0] != filepath.Join(dir, "main.py") || args[1] != "--quiet" {
		t.Errorf("%s %v", program, args)
	}
}

// BenchmarkExecute measures one action.execute round trip to a running plugin.
func BenchmarkExecute(b *testing.B) {
	exe, err := os.Executable()
	if err != nil {
		b.Fatal(err)
	}
	b.Setenv(plugintest.ModeEnv, "normal")
	p := &model.Plugin{Name: "demo", Namespace: "demo", Dir: b.TempDir(), Executable: exe}
	pr, err := Start(context.Background(), p, Options{ProjectDir: b.TempDir()})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = pr.Close() }()
	config := map[string]any{"text": "bench"}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := pr.Execute(context.Background(), "demo.echo", config, ExecContext{RunID: "R"}); err != nil {
			b.Fatal(err)
		}
	}
}
