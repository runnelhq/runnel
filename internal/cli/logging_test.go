package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

// --log-format json writes the run log as an object per line; --log-level warn leaves
// out info; -v and --log-level debug show the details (S71).
func TestRunLogOptions(t *testing.T) {
	chdirRepoRoot(t)
	t.Setenv("COMPUTERNAME", "TESTPC")
	t.Setenv("TEMP", `C:\Temp`)
	const wf = "examples/hello/workflows/hello.workflow.yaml"
	code, out, _ := run(t, "run", "--log-format", "json", "--log-level", "debug", wf)
	if code != 0 {
		t.Fatalf("code %d:\n%s", code, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var actions int
	for _, l := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil || m["msg"] == nil || m["level"] == nil || m["time"] == nil {
			t.Fatalf("not a JSON line: %q (%v)", l, err)
		}
		if m["action"] != nil {
			actions++
		}
	}
	if actions == 0 || !strings.Contains(out, `"level":"DEBUG"`) {
		t.Errorf("no action or debug lines:\n%s", out)
	}
	_, debug, _ := run(t, "run", "-v", wf)
	_, viaLevel, _ := run(t, "run", "--log-level", "debug", wf)
	if strings.Count(debug, "\n") != strings.Count(viaLevel, "\n") {
		t.Errorf("-v and --log-level debug differ:\n%s\n---\n%s", debug, viaLevel)
	}
	code, out, _ = run(t, "run", "--log-level", "warn", wf)
	if code != 0 || strings.Contains(out, "INFO") {
		t.Errorf("warn: %d\n%s", code, out)
	}
	for _, args := range [][]string{{"--log-level", "trace"}, {"--log-format", "xml"}} {
		code, _, errOut := run(t, append(append([]string{"run"}, args...), wf)...)
		if code != 2 || !strings.Contains(errOut, args[0]+": invalid") {
			t.Errorf("%v: %d %s", args, code, errOut)
		}
	}
}

// The log options of service install go to the command line of the service; a log
// file of its own is made absolute and its folder prepared.
func TestServiceLogOptions(t *testing.T) {
	o := installOptions{name: "runnel", account: "localservice", start: "delayed", port: 7300, stopTimeout: 30 * time.Second,
		log: logOptions{level: "warn", format: "json", maxSize: "50MB", files: 10, file: "logs/runnel.log"}}
	c, err := serviceConfig(o, `D:\flows`, `C:\Tools\runnel.exe`)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(c.Args, "--log-level")
	if i < 0 || !slices.Equal(c.Args[i:i+8], []string{"--log-level", "warn", "--log-format", "json", "--log-max-size", "50MB", "--log-file", c.Args[i+7]}) ||
		!strings.HasSuffix(c.Args[i+7], "runnel.log") || !slices.Contains(c.Args, "--log-files") || c.Args[len(c.Args)-1] != `D:\flows` {
		t.Errorf("args %q", c.Args)
	}
	for _, tt := range []struct {
		change func(*logOptions)
		want   string
	}{
		{func(l *logOptions) { l.level = "loud" }, "--log-level: invalid log level"},
		{func(l *logOptions) { l.maxSize = "10" }, "--log-max-size"},
		{func(l *logOptions) { l.maxSize = "100B" }, "less than 1KB"},
		{func(l *logOptions) { l.files = 1000 }, "invalid --log-files 1000"},
	} {
		bad := o
		tt.change(&bad.log)
		if _, err := serviceConfig(bad, `D:\flows`, "runnel.exe"); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("got %v, want %q", err, tt.want)
		}
	}
}
