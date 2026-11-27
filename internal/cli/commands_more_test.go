package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/winsvc"
)

// HTTP and file triggers start runs on events, not at a time: their column NEXT says so.
func TestTriggersEvents(t *testing.T) {
	chdirRepoRoot(t)
	for _, tt := range []struct{ dir, name, next string }{
		{"examples/hello", "greet", "on request"},
		{"examples/files", "orders", "on new files"},
	} {
		code, out, errOut := run(t, "triggers", tt.dir)
		if code != 0 || errOut != "" {
			t.Fatalf("%s: %d %s", tt.dir, code, errOut)
		}
		found := false
		for line := range strings.SplitSeq(out, "\n") {
			if f := strings.Fields(line); len(f) > 0 && f[0] == tt.name {
				found = strings.HasSuffix(line, tt.next)
			}
		}
		if !found {
			t.Errorf("%s: no line %s … %s in\n%s", tt.dir, tt.name, tt.next, out)
		}
	}
}

// The next steps of "runnel init" quote a folder with a space, so that they can be
// copied into a console.
func TestInitFolderWithSpace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my flows")
	code, out, errOut := run(t, "init", "--name", "flows", dir)
	if code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"  runnel validate \"" + dir + "\"\n", "  runnel serve \"" + dir + "\"\n", "  runnel run \"" + filepath.Join(dir, "workflows")} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "project.yaml")); err != nil {
		t.Error(err)
	}
}

// A server serving a folder without project reports no name (R10): "none"; a report
// without status is no health report (exit code 3).
func TestHealthNoProject(t *testing.T) {
	for _, tt := range []struct {
		body string
		code int
		want string
	}{
		{`{"status": "ok", "version": "v1", "startedAt": "2026-11-26T08:00:00Z", "project": {"dir": "D:\\inbox"}, "checks": []}`, 0, "Project:  none (D:\\inbox)\n"},
		{`{"version": "v1"}`, 3, "without a health report"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(tt.body))
		}))
		code, out, errOut := run(t, "health", "--addr", srv.Listener.Addr().String())
		srv.Close()
		if code != tt.code || !strings.Contains(out+errOut, tt.want) {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q; want %d and %q", tt.body, code, out, errOut, tt.code, tt.want)
		}
	}
}

// A service with its own history folder (shared with "runnel serve" of a user, ADR 0026)
// and log file: both as absolute paths in its command line; the default name gives the
// display name "Runnel", no TLS needs no certificate folder.
func TestServiceConfigFolders(t *testing.T) {
	t.Chdir(t.TempDir())
	o := installOptions{name: winsvc.DefaultName, account: "localservice", start: "auto", port: 7300, stopTimeout: 30 * time.Second,
		historyDir: "hist", log: logOptions{level: "info", format: "text", maxSize: "10MB", file: filepath.Join("logs", "svc.log")}}
	c, err := serviceConfig(o, `D:\flows`, "runnel.exe")
	if err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	hist, logDir := filepath.Join(wd, "hist"), filepath.Join(wd, "logs")
	i := slices.Index(c.Args, "--history-dir")
	if i < 0 || c.Args[i+1] != hist || c.HistoryDir != hist || !c.HistoryShared {
		t.Errorf("history: args %q, dir %s, shared %v", c.Args, c.HistoryDir, c.HistoryShared)
	}
	if c.LogDir != logDir || !slices.Contains(c.Args, filepath.Join(logDir, "svc.log")) || slices.Contains(c.Args, "--log-files") {
		t.Errorf("log: args %q, dir %s", c.Args, c.LogDir)
	}
	if c.DisplayName != "Runnel" || c.TLSDir != "" || c.Start != "auto" {
		t.Errorf("config %+v", c)
	}
	o.historyDir = ""
	if c, err := serviceConfig(o, `D:\flows`, "runnel.exe"); err != nil || c.HistoryShared || c.HistoryDir != winsvc.HistoryDir() || slices.Contains(c.Args, "--history-dir") {
		t.Errorf("default history: %+v %v", c, err)
	}
}

// The listener of http triggers reports a folder for its certificate that cannot be
// created; the service gets the paths of an own certificate as absolute paths.
func TestListenerCertificatePaths(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	writeFile(t, file, "x")
	o := listenerOptions{Address: "127.0.0.1:0", TLS: "auto"}
	if _, err := o.tlsConfig(filepath.Join(file, "tls"), func(string) {}, func(string) {}); err == nil || !strings.Contains(err.Error(), "self-signed certificate in "+filepath.Join(file, "tls")) {
		t.Errorf("folder below a file: %v", err)
	}
	t.Chdir(t.TempDir())
	wd, _ := os.Getwd()
	abs, err := listenerOptions{Address: "0.0.0.0:7301", Cert: "cert.pem", Key: filepath.Join("keys", "key.pem")}.absolute()
	if err != nil || abs.Cert != filepath.Join(wd, "cert.pem") || abs.Key != filepath.Join(wd, "keys", "key.pem") || abs.Address != "0.0.0.0:7301" {
		t.Errorf("absolute: %+v %v", abs, err)
	}
}
