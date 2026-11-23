package proc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The test binary also serves as the program to run: with RUNNEL_PROC_HELPER set, TestMain
// does what the mode says instead of running the tests.
func TestMain(m *testing.M) {
	if mode := os.Getenv("RUNNEL_PROC_HELPER"); mode != "" {
		os.Exit(helper(mode))
	}
	os.Exit(m.Run())
}

func helper(mode string) int {
	switch mode {
	case "echo": // arguments to stdout, stdin to stderr, exit code from EXIT
		fmt.Println(strings.Join(os.Args[1:], "|"))
		in, _ := os.ReadFile("/dev/stdin")
		if len(in) == 0 {
			in = readAll()
		}
		fmt.Fprint(os.Stderr, string(in))
		var code int
		_, _ = fmt.Sscan(os.Getenv("EXIT"), &code)
		return code
	case "flood": // 3 MB to stdout
		chunk := strings.Repeat("x", 1024)
		for range 3 * 1024 {
			fmt.Print(chunk)
		}
		return 0
	case "tree": // starts a child that writes a heartbeat file, then waits
		self, _ := os.Executable()
		var childEnv []string
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "RUNNEL_PROC_HELPER=") {
				childEnv = append(childEnv, e)
			}
		}
		child, err := os.StartProcess(self, []string{self}, &os.ProcAttr{
			Env: append(childEnv, "RUNNEL_PROC_HELPER=beat"), Files: []*os.File{nil, nil, nil}})
		if err != nil {
			return 3
		}
		_ = child.Release()
		time.Sleep(time.Minute)
		return 0
	case "beat":
		for range 1200 {
			_ = os.WriteFile(os.Getenv("BEAT"), []byte(time.Now().String()), 0o600)
			time.Sleep(50 * time.Millisecond)
		}
		return 0
	}
	return 2
}

// readAll reads stdin where /dev/stdin does not exist (Windows).
func readAll() []byte {
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			return []byte(b.String())
		}
	}
}

func self(t *testing.T) string {
	p, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func env(mode string, more ...string) []string {
	return append(append(os.Environ(), "RUNNEL_PROC_HELPER="+mode), more...)
}

func TestRun(t *testing.T) {
	// Arguments arrive one by one, also with spaces and quotes; stdin; the exit code is no error.
	res, err := Run(context.Background(), Command{Program: self(t), Args: []string{"a b", `"q"`, "&"},
		Env: env("echo", "EXIT=3"), Input: "hello", HasInput: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 3 || strings.TrimSpace(res.Stdout) != `a b|"q"|&` || res.Stderr != "hello" || res.Truncated {
		t.Errorf("%+v", res)
	}
	// Without input stdin is empty, so that a program waiting for input does not hang.
	res, err = Run(context.Background(), Command{Program: self(t), Env: env("echo"), Timeout: 10 * time.Second})
	if err != nil || res.Stderr != "" || res.ExitCode != 0 {
		t.Errorf("%+v %v", res, err)
	}
}

func TestRunLimitsOutput(t *testing.T) {
	res, err := Run(context.Background(), Command{Program: self(t), Env: env("flood"), MaxOutput: 1000})
	if err != nil || !res.Truncated || len(res.Stdout) != 1000 || res.ExitCode != 0 {
		t.Errorf("%d bytes, truncated %v, %v", len(res.Stdout), res.Truncated, err)
	}
}

// A timeout ends the program and the processes it started.
func TestRunTimeoutEndsTree(t *testing.T) {
	beat := filepath.Join(t.TempDir(), "beat")
	start := time.Now()
	_, err := Run(context.Background(), Command{Program: self(t), Env: env("tree", "BEAT="+beat), Timeout: 500 * time.Millisecond})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("%v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %s", d)
	}
	// The child wrote its heartbeat while running; after the end it stops.
	time.Sleep(300 * time.Millisecond)
	before, err := os.ReadFile(beat)
	if err != nil {
		t.Fatalf("the child never ran: %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	after, _ := os.ReadFile(beat)
	if string(before) != string(after) {
		t.Error("the child still runs after the timeout")
	}

	// Cancelling the run ends it the same way.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := Run(ctx, Command{Program: self(t), Env: env("tree", "BEAT="+beat)}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("cancelled: %v", err)
	}
}

func TestCheckBatchArgs(t *testing.T) {
	if err := CheckBatchArgs("run.exe", []string{"a & b"}); err != nil {
		t.Error(err)
	}
	if err := CheckBatchArgs(`C:\x\run.BAT`, []string{"ok", "a & del *"}); err == nil || !strings.Contains(err.Error(), `args[1]: "a & del *" contains "&"`) {
		t.Errorf("%v", err)
	}
	for _, a := range []string{"100%", `"x"`, "a|b", "a^b", "!x!", "a\nb"} {
		if err := CheckBatchArgs("x.cmd", []string{a}); err == nil {
			t.Errorf("%q accepted", a)
		}
	}
	// A batch file through Run is rejected before it starts.
	if _, err := Run(context.Background(), Command{Program: "x.bat", Args: []string{"a>b"}}); err == nil {
		t.Error("Run started a batch file with > in an argument")
	}
}

func TestResolve(t *testing.T) {
	dir := t.TempDir()
	name := "tool"
	if filepath.Separator == '\\' {
		name = "tool.exe"
	}
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if p, err := Resolve("bin/"+name, dir); err != nil || p != filepath.Join(dir, "bin", name) {
		t.Errorf("%q %v", p, err)
	}
	if _, err := Resolve("no-such-program-runnel", dir); err == nil || err.Error() != `program "no-such-program-runnel" not found in PATH` {
		t.Errorf("%v", err)
	}
	if _, err := Resolve("bin/missing", dir); err == nil {
		t.Error("missing file resolved")
	}
}

func TestDecode(t *testing.T) {
	latin := []byte{'K', 0xf6, 'l', 'n'} // "Köln" in Windows-1252
	if got := decode(latin, EncodingWindows1252); got != "Köln" {
		t.Errorf("windows-1252: %q", got)
	}
	if got := decode([]byte("Köln"), EncodingAuto); got != "Köln" {
		t.Errorf("auto utf-8: %q", got)
	}
	if got := decode([]byte{0x94}, EncodingOEM); oemCodePage() == 0 && got != "”" {
		t.Errorf("oem outside Windows is Windows-1252: %q", got)
	}
	if got := decode(latin, EncodingUTF8); got != "K\uFFFDln" {
		t.Errorf("utf-8: %q", got)
	}
}
