package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/runs"
	"github.com/runnelhq/runnel/internal/tlscert"
)

const failingWorkflow = `kind: Workflow
apiVersion: runnel/v1
name: broken
actions:
  - { id: start, type: core.start }
  - { id: stop, type: core.abort, with: { message: boom } }
links:
  - { from: start, to: stop }
`

// healthServer creates a project with a run history and a server whose scheduler runs;
// the disk has plenty of space unless the test says otherwise.
func healthServer(t *testing.T, opts Options, files map[string]string) *Server {
	t.Helper()
	dir, _ := newRunProject(t)
	for name, content := range files {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(name)), content)
	}
	opts.Dir, opts.Registry, opts.UI, opts.HistoryDir = dir, actions.Builtin(), fstest.MapFS{}, t.TempDir()
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	s.diskFree = func(string) (uint64, uint64, error) { return 50 << 30, 100 << 30, nil }
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = s.Shutdown(context.Background())
		_ = s.Close()
	})
	s.StartTriggers(ctx)
	want := 0
	for name := range files {
		if strings.HasPrefix(name, "triggers/") {
			want++
		}
	}
	waitFor(t, "the scheduler", func() bool {
		return s.triggers.Running() && len(s.triggers.List())+len(problemFiles(s.response(s.triggers.Problems()).Problems)) == want
	})
	return s
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !ok(); {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func getHealth(t *testing.T, s *Server, wantCode int) Health {
	t.Helper()
	rec := do(t, s, "GET", "/api/v1/health", "")
	var h Health
	if err := json.Unmarshal(rec.Body.Bytes(), &h); err != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec.Code != wantCode {
		t.Errorf("status %d, want %d: %s", rec.Code, wantCode, rec.Body)
	}
	return h
}

// checkOf returns the check with the name; Status is empty if there is none.
func checkOf(h Health, name string) HealthCheck {
	for _, c := range h.Checks {
		if c.Name == name {
			return c
		}
	}
	return HealthCheck{}
}

func TestHealthOK(t *testing.T) {
	s := healthServer(t, Options{}, map[string]string{
		"triggers/often.yaml": "kind: Trigger\napiVersion: runnel/v1\nname: often\ntype: interval\ntarget: workflows/wait.workflow.yaml\nwith: { every: 1h }\n",
	})
	h := getHealth(t, s, http.StatusOK)
	if h.Status != HealthOK || h.Project.Name != "demo" || h.Project.Dir != s.dir || h.Version == "" || h.StartedAt.IsZero() {
		t.Errorf("health %+v", h)
	}
	if !h.Scheduler.Running || h.Scheduler.Triggers != 1 || h.Scheduler.Active != 1 || len(h.Scheduler.Problems) != 0 {
		t.Errorf("scheduler %+v", h.Scheduler)
	}
	if !h.History.Enabled || !h.History.Writable || h.History.SizeBytes == 0 || !strings.HasSuffix(h.History.Path, "runs.db") {
		t.Errorf("history %+v", h.History)
	}
	if h.Disk.FreeBytes != 50<<30 || h.Disk.Path == "" || h.Listener != nil {
		t.Errorf("disk %+v, listener %+v", h.Disk, h.Listener)
	}
	for _, name := range []string{"scheduler", "triggers", "runs", "history", "disk"} {
		if c := checkOf(h, name); c.Status != HealthOK || c.Message == "" {
			t.Errorf("check %s: %+v", name, c)
		}
	}
	if c := checkOf(h, "scheduler"); c.Message != "running, 1 of 1 triggers active" {
		t.Errorf("scheduler: %q", c.Message)
	}
	if rec := do(t, s, "GET", "/api/v1/health", ""); rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control %q", rec.Header().Get("Cache-Control"))
	}
}

func TestHealthBrokenTrigger(t *testing.T) {
	s := healthServer(t, Options{}, map[string]string{
		"triggers/bad.yaml": "kind: Trigger\napiVersion: runnel/v1\nname: bad\ntype: interval\ntarget: workflows/missing.workflow.yaml\nwith: { every: 1h }\n",
	})
	h := getHealth(t, s, http.StatusOK)
	c := checkOf(h, "triggers")
	if h.Status != HealthDegraded || c.Status != HealthDegraded || !strings.Contains(c.Message, "triggers/bad.yaml") {
		t.Errorf("status %s, check %+v", h.Status, c)
	}
	if len(h.Scheduler.Problems) == 0 || h.Scheduler.Problems[0].File != "triggers/bad.yaml" || h.Scheduler.Problems[0].Message == "" {
		t.Errorf("problems %+v", h.Scheduler.Problems)
	}
}

func TestHealthCertificate(t *testing.T) {
	// A self-signed certificate made Validity minus 10 days ago ends in 10 days.
	cert, _, err := tlscert.SelfSigned(t.TempDir(), []string{"localhost"}, nil, time.Now().Add(-tlscert.Validity+10*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	s := healthServer(t, Options{TriggerListen: "127.0.0.1:0", TriggerTLS: tlscert.Static(cert)}, nil)
	h := getHealth(t, s, http.StatusOK)
	c := checkOf(h, "certificate")
	if h.Status != HealthDegraded || c.Status != HealthDegraded || !strings.Contains(c.Message, "expires in 9 days") {
		t.Errorf("status %s, check %+v", h.Status, c)
	}
	l := h.Listener
	if l == nil || l.Configured != "127.0.0.1:0" || l.Running || !l.TLS || l.NotAfter == nil || !l.NotAfter.Equal(cert.Leaf.NotAfter) {
		t.Fatalf("listener %+v", l)
	}
	if c := checkOf(h, "listener"); c.Status != HealthOK || !strings.Contains(c.Message, "no http trigger") {
		t.Errorf("listener %+v", c)
	}

	// Expired; a certificate valid for long is fine.
	s.now = func() time.Time { return cert.Leaf.NotAfter.Add(time.Hour) }
	if c := checkOf(getHealth(t, s, http.StatusOK), "certificate"); c.Status != HealthDegraded || !strings.Contains(c.Message, "has expired") {
		t.Errorf("expired: %+v", c)
	}
	s.now = func() time.Time { return cert.Leaf.NotAfter.Add(-30 * 24 * time.Hour) }
	if h := getHealth(t, s, http.StatusOK); h.Status != HealthOK || checkOf(h, "certificate").Status != HealthOK {
		t.Errorf("valid: %+v", h.Checks)
	}
}

func TestHealthLowDisk(t *testing.T) {
	s := healthServer(t, Options{}, nil)
	for _, c := range []struct {
		free, total uint64
		want        string
	}{
		{900 << 20, 500 << 30, HealthDegraded},     // below 1 GiB
		{2 << 30, 500 << 30, HealthOK},             // 2 GiB free on a large disk
		{300 << 20, 8 << 30, HealthDegraded},       // below 5 % of a small disk
		{500 << 20, 8 << 30, HealthOK},             // above 5 % of a small disk
		{0, 0, HealthOK},                           // unknown size: nothing to compare
		{1 << 40, 2 << 40, HealthOK},               // plenty
		{(1 << 30) - 1, 100 << 30, HealthDegraded}, // just below 1 GiB
	} {
		s.diskFree = func(string) (uint64, uint64, error) { return c.free, c.total, nil }
		h := getHealth(t, s, http.StatusOK)
		if got := checkOf(h, "disk"); got.Status != c.want || h.Status != c.want {
			t.Errorf("free %d of %d: %s (%+v), want %s", c.free, c.total, h.Status, got, c.want)
		}
	}
	s.diskFree = func(string) (uint64, uint64, error) { return 0, 0, errDiskTest }
	if h := getHealth(t, s, http.StatusOK); h.Status != HealthOK || h.Disk.Error == "" {
		t.Errorf("unknown: %+v", h.Disk)
	}
}

var errDiskTest = errors.New("no disk")

func TestHealthFailedTriggerRun(t *testing.T) {
	s := healthServer(t, Options{}, map[string]string{
		"workflows/broken.workflow.yaml": failingWorkflow,
		"triggers/nightly.yaml":          "kind: Trigger\napiVersion: runnel/v1\nname: nightly\ntype: cron\ntarget: workflows/broken.workflow.yaml\nwith: { schedule: \"0 3 * * *\" }\n",
	})
	if h := getHealth(t, s, http.StatusOK); h.Status != HealthOK {
		t.Fatalf("before: %+v", h.Checks)
	}
	s.triggers.Tick(s.triggers.List()[0].Next)
	id := s.triggers.List()[0].Last.RunID
	waitRun(t, s, id, func(d runs.Detail) bool { return d.Status != runs.StatusRunning })
	waitFor(t, "the end of the run", func() bool { return s.runs.Health().Triggers["cron:nightly"] != "" })

	h := getHealth(t, s, http.StatusOK)
	c := checkOf(h, "runs")
	if h.Status != HealthDegraded || c.Status != HealthDegraded || !strings.Contains(c.Message, "nightly (failed)") {
		t.Errorf("status %s, check %+v", h.Status, c)
	}
	f := h.Runs.LastFailed
	if f == nil || f.ID != id || f.File != "workflows/broken.workflow.yaml" || f.Trigger != "cron:nightly" || f.Time.IsZero() || !strings.Contains(f.Error, "boom") {
		t.Errorf("last failed %+v", f)
	}

	// A failed run started by hand is reported, but does not degrade the status.
	s2 := healthServer(t, Options{}, map[string]string{"workflows/broken.workflow.yaml": failingWorkflow})
	run := startRun(t, s2, `{"path": "workflows/broken.workflow.yaml"}`)
	waitRun(t, s2, run.ID, func(d runs.Detail) bool { return d.Status != runs.StatusRunning })
	waitFor(t, "the end of the run", func() bool { return s2.runs.Health().LastFailed != nil })
	if h := getHealth(t, s2, http.StatusOK); h.Status != HealthOK || h.Runs.LastFailed == nil || h.Runs.LastFailed.Trigger != "" {
		t.Errorf("by hand: %s %+v", h.Status, h.Runs.LastFailed)
	}
}

func TestHealthFail(t *testing.T) {
	// The scheduler does not run.
	_, s := newProject(t)
	h := getHealth(t, s, http.StatusServiceUnavailable)
	if h.Status != HealthFail || checkOf(h, "scheduler").Status != HealthFail {
		t.Errorf("no scheduler: %+v", h.Checks)
	}

	// The history cannot be written.
	s = healthServer(t, Options{}, nil)
	_ = s.history.Close()
	run := startRun(t, s, `{"path": "workflows/wait.workflow.yaml", "environment": "dev"}`)
	waitRun(t, s, run.ID, func(d runs.Detail) bool { return d.Status != runs.StatusRunning })
	waitFor(t, "a write error", func() bool { return s.runs.Health().HistoryError != "" })
	h = getHealth(t, s, http.StatusServiceUnavailable)
	c := checkOf(h, "history")
	if h.Status != HealthFail || c.Status != HealthFail || h.History.Writable || h.History.Error == "" || !strings.Contains(c.Message, "cannot be written") {
		t.Errorf("status %s, check %+v, history %+v", h.Status, c, h.History)
	}
}

// A history that cannot be opened fails the health; without a history it is fine.
func TestHealthHistoryOff(t *testing.T) {
	dir, s := newRunProject(t)
	if c := checkOf(s.health(), "history"); c.Status != HealthOK || !strings.Contains(c.Message, "off") {
		t.Errorf("off: %+v", c)
	}
	blocked := filepath.Join(t.TempDir(), "file")
	writeFile(t, blocked, "not a folder")
	s, err := New(Options{Dir: dir, Registry: actions.Builtin(), UI: fstest.MapFS{}, HistoryDir: blocked})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()); _ = s.Close() })
	h := s.health()
	if c := checkOf(h, "history"); c.Status != HealthFail || !h.History.Enabled || h.History.Error == "" {
		t.Errorf("cannot be opened: %+v %+v", c, h.History)
	}
}

// GET /health is protected like the other endpoints: no token, but only local clients.
func TestHealthGuard(t *testing.T) {
	_, s := newProject(t)
	expect(t, do(t, s, "GET", "/api/v1/health", "", "Host", "evil.example:7300"), http.StatusForbidden, "localhost or 127.0.0.1")
	expect(t, do(t, s, "GET", "/api/v1/health", "", "Host", "192.168.1.10:7300"), http.StatusForbidden, "localhost or 127.0.0.1")
	expect(t, do(t, s, "GET", "/api/v1/health", "", "Origin", "http://evil.example"), http.StatusForbidden, "other origins")
	expect(t, do(t, s, "POST", "/api/v1/health", ""), http.StatusNotFound, "unknown endpoint")
	expect(t, do(t, s, "GET", "/api/v1/health", "", "Host", "localhost:7300"), http.StatusServiceUnavailable, `"status": "fail"`)
}

// BenchmarkHealth measures GET /health of a server with a history and a few triggers.
//
//	go test ./internal/server -run XXX -bench Health -benchmem
func BenchmarkHealth(b *testing.B) {
	dir := filepath.Join(b.TempDir(), "demo")
	writeFile(b, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: demo\n")
	writeFile(b, filepath.Join(dir, "workflows", "w.workflow.yaml"), workflowYAML)
	for _, name := range []string{"a", "b", "c", "d"} {
		writeFile(b, filepath.Join(dir, "triggers", name+".yaml"), "kind: Trigger\napiVersion: runnel/v1\nname: "+name+
			"\ntype: interval\ntarget: workflows/w.workflow.yaml\nwith: { every: 1h }\n")
	}
	s, err := New(Options{Dir: dir, Registry: actions.Builtin(), UI: fstest.MapFS{}, HistoryDir: b.TempDir()})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = s.Shutdown(context.Background()); _ = s.Close() }()
	s.triggers.Reload()
	h := s.Handler()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Host = "127.0.0.1:7300"
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}
