package cli

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/server"
)

// healthBody is an answer of GET /health; status and checks are set by the test.
const healthBody = `{
  "status": "%STATUS%",
  "version": "v1.2.3",
  "uptime": 7384,
  "startedAt": "2026-11-26T08:00:00+01:00",
  "project": {"name": "flows", "dir": "D:\\flows"},
  "checks": [
    {"name": "scheduler", "status": "%SCHEDULER%", "message": "%SCHEDMSG%"},
    {"name": "triggers", "status": "%TRIGGERS%", "message": "%TRIGGERMSG%"},
    {"name": "runs", "status": "ok", "message": "0 running"}
  ],
  "scheduler": {"running": true, "triggers": 3, "active": 2,
    "problems": [%PROBLEMS%]},
  "runs": {"running": 0, "onErrorQueue": 0,
    "lastFailed": {"id": "01JD", "time": "2026-11-26T09:30:00+01:00", "file": "workflows/import.workflow.yaml", "trigger": "cron:nightly", "error": "boom\nsecond line"}},
  "history": {"enabled": true, "writable": true, "sizeBytes": 1024},
  "disk": {"path": "C:\\ProgramData\\runnel\\history", "freeBytes": 1, "totalBytes": 2}
}`

const badTrigger = `{"file": "triggers/bad.yaml", "line": 4, "column": 9, "severity": "error", "message": "target workflows/x.workflow.yaml does not exist"}`

// healthStub serves a health report with the status (and its HTTP status code).
func healthStub(t *testing.T, status string) string {
	t.Helper()
	body := strings.NewReplacer("%STATUS%", status, "%SCHEDULER%", map[bool]string{true: "fail", false: "ok"}[status == "fail"],
		"%TRIGGERS%", map[bool]string{true: "ok", false: "degraded"}[status == "ok"],
		"%TRIGGERMSG%", map[bool]string{true: "all trigger files loaded", false: "1 trigger file with errors, not loaded: triggers/bad.yaml"}[status == "ok"],
		"%PROBLEMS%", map[bool]string{true: "", false: badTrigger}[status == "ok"],
		"%SCHEDMSG%", map[bool]string{true: "the scheduler of the triggers is not running", false: "running, 2 of 3 triggers active"}[status == "fail"]).Replace(healthBody)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/health" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		code := http.StatusOK
		if status == "fail" {
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String()
}

func TestHealth(t *testing.T) {
	var all strings.Builder
	for _, c := range []struct {
		status string
		code   int
	}{{"ok", 0}, {"degraded", 1}, {"fail", 2}} {
		addr := healthStub(t, c.status)
		code, out, errOut := run(t, "health", "--addr", addr)
		if code != c.code || errOut != "" {
			t.Errorf("%s: exit code %d, want %d; stderr %q", c.status, code, c.code, errOut)
		}
		all.WriteString("$ runnel health  # " + c.status + "\n" + out)
	}
	compareGoldenFile(t, filepath.Join("testdata", "health.golden"), all.String())
}

func TestHealthJSON(t *testing.T) {
	code, out, _ := run(t, "health", "--json", "--addr", healthStub(t, "degraded"))
	var h server.Health
	if err := json.Unmarshal([]byte(out), &h); err != nil || code != 1 {
		t.Fatalf("exit code %d, output %q: %v", code, out, err)
	}
	if h.Status != "degraded" || len(h.Checks) != 3 || h.Runs.LastFailed == nil || h.Scheduler.Problems[0].File != "triggers/bad.yaml" {
		t.Errorf("health %+v", h)
	}
}

// Exit code 3 when the health is unknown: no server, another program, wrong options.
func TestHealthUnknown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := ln.Addr().String()
	_ = ln.Close()
	other := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(other.Close)

	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--addr", closed}, "no runnel server reachable at " + closed},
		{[]string{"--addr", other.Listener.Addr().String()}, "without a health report"},
		{[]string{"--addr", "192.168.1.10:7300"}, "listens on this computer only"},
		{[]string{"--addr", "example.com:7300"}, "listens on this computer only"},
		{[]string{"--bogus"}, "unknown flag: --bogus"},
		{[]string{"--timeout", "soon"}, "invalid argument"},
		{[]string{"--service", "no such name"}, "invalid service name"},
	}
	if runtime.GOOS != "windows" {
		cases = append(cases, struct {
			args []string
			want string
		}{[]string{"--service", "runnel"}, "only available on Windows"})
	}
	for _, c := range cases {
		code, out, errOut := run(t, append([]string{"health"}, c.args...)...)
		if code != 3 || out != "" || !strings.Contains(errOut, c.want) {
			t.Errorf("%v: exit code %d, stdout %q, stderr %q; want 3 and %q", c.args, code, out, errOut, c.want)
		}
	}
}

func TestServicePort(t *testing.T) {
	for cmd, want := range map[string]int{
		`"C:\runnel\runnel.exe" service run --name runnel --port 7310 --triggers 127.0.0.1:7301 D:\flows`: 7310,
		`"C:\runnel\runnel.exe" service run --name runnel --port=7320 D:\flows`:                           7320,
		`"C:\runnel\runnel.exe" service run --name runnel D:\flows`:                                       DefaultPort,
		`runnel.exe service run --port 99999 D:\flows`:                                                    0,
	} {
		got, err := portOf("runnel", cmd)
		if got != want || (err != nil) != (want == 0) {
			t.Errorf("%s: port %d (%v), want %d", cmd, got, err, want)
		}
	}
}
