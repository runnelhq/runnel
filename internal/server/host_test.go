package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/runnelhq/runnel/internal/actions"
)

// newHost returns a host of a new folder "My Flows" without project (R10).
func newHost(t *testing.T) (string, *Host) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "My Flows")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	h, err := Open(Options{Dir: dir, Registry: actions.Builtin(), UI: fstest.MapFS{"index.html": {Data: []byte("editor")}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.StartTriggers(ctx)
	t.Cleanup(func() {
		cancel()
		_ = h.Shutdown(context.Background())
		_ = h.Close()
	})
	return dir, h
}

func doHost(t *testing.T, h *Host, method, target, body string, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = "127.0.0.1:7300"
	for i := 0; i+1 < len(header); i += 2 {
		if header[i] == "Host" {
			req.Host = header[i+1]
		} else {
			req.Header.Set(header[i], header[i+1])
		}
	}
	rec := httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, req)
	return rec
}

func TestHostWithoutProject(t *testing.T) {
	dir, h := newHost(t)
	if h.HasProject() {
		t.Fatal("HasProject without project.yaml")
	}
	rec := doHost(t, h, "GET", "/api/v1/project", "")
	var st ProjectStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
	if st != (ProjectStatus{Dir: dir, DefaultName: "my-flows", Empty: true}) {
		t.Errorf("status %+v", st)
	}
	expect(t, doHost(t, h, "GET", "/", ""), http.StatusOK, "editor")
	expect(t, doHost(t, h, "GET", "/api/v1/version", ""), http.StatusOK, `"version"`)
	for _, target := range []string{"/api/v1/projects/current", "/api/v1/projects/current/tree", "/api/v1/files/project.yaml", "/api/v1/runs", "/api/v1/triggers"} {
		expect(t, doHost(t, h, "GET", target, ""), http.StatusConflict, `"code": "no_project"`)
	}
	expect(t, doHost(t, h, "PUT", "/api/v1/files/project.yaml", "kind: Project"), http.StatusConflict, `"code": "no_project"`)
	if _, err := os.Stat(filepath.Join(dir, "project.yaml")); err == nil {
		t.Error("PUT wrote a file without project")
	}

	rec = doHost(t, h, "GET", "/api/v1/health", "")
	var hl Health
	if err := json.Unmarshal(rec.Body.Bytes(), &hl); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("health: status %d, body %s", rec.Code, rec.Body)
	}
	if hl.Status != HealthDegraded || hl.Project.Dir != dir || len(hl.Checks) == 0 || hl.Checks[0].Name != "project" || hl.Scheduler.Running {
		t.Errorf("health %+v", hl)
	}
}

func TestHostCreateProject(t *testing.T) {
	dir, h := newHost(t)
	rec := doHost(t, h, "POST", "/api/v1/project", `{"name": "flows"}`, "Origin", "http://127.0.0.1:7300")
	expect(t, rec, http.StatusCreated, `"name": "flows"`)
	if !strings.Contains(rec.Body.String(), "workflows/hello.workflow.yaml") || !h.HasProject() {
		t.Errorf("body %s", rec.Body)
	}
	// The project is served at once: files, the project and the scheduler.
	expect(t, doHost(t, h, "GET", "/api/v1/projects/current", ""), http.StatusOK, `"name": "flows"`)
	expect(t, doHost(t, h, "GET", "/api/v1/files/workflows/hello.workflow.yaml", ""), http.StatusOK, "core.start")
	expect(t, doHost(t, h, "PUT", "/api/v1/files/data/a.txt", "x", "If-None-Match", "*"), http.StatusCreated, "")
	expect(t, doHost(t, h, "GET", "/api/v1/project", ""), http.StatusOK, `"exists": true`)
	waitFor(t, "scheduler running", func() bool {
		return strings.Contains(doHost(t, h, "GET", "/api/v1/health", "").Body.String(), `"running": true`)
	})
	if _, err := os.Stat(filepath.Join(dir, "data", "a.txt")); err != nil {
		t.Error(err)
	}
	// A second project is refused.
	expect(t, doHost(t, h, "POST", "/api/v1/project", `{}`), http.StatusConflict, `"code": "project_exists"`)
}

// Triggers of files that were there before are loaded once the project exists.
func TestHostCreateProjectWithFiles(t *testing.T) {
	dir, h := newHost(t)
	writeFile(t, filepath.Join(dir, "triggers", "hourly.yaml"), "kind: Trigger\napiVersion: runnel/v1\nname: hourly\ntype: interval\n"+
		"target: workflows/hello.workflow.yaml\nwith:\n  every: 1h\n")
	expect(t, doHost(t, h, "GET", "/api/v1/project", ""), http.StatusOK, `"empty": false`)
	expect(t, doHost(t, h, "POST", "/api/v1/project", ``), http.StatusConflict, `"code": "not_empty"`)
	if _, err := os.Stat(filepath.Join(dir, "project.yaml")); err == nil {
		t.Fatal("project.yaml written into a folder that is not empty")
	}
	rec := doHost(t, h, "POST", "/api/v1/project", `{"force": true}`)
	expect(t, rec, http.StatusCreated, `"skipped": [`)
	if !strings.Contains(rec.Body.String(), `"triggers/"`) {
		t.Errorf("body %s", rec.Body)
	}
	waitFor(t, "trigger loaded", func() bool {
		return strings.Contains(doHost(t, h, "GET", "/api/v1/triggers", "").Body.String(), `"name": "hourly"`)
	})
}

func TestHostCreateProjectRejected(t *testing.T) {
	dir, h := newHost(t)
	for _, c := range []struct {
		name, body string
		header     []string
		status     int
		want       string
	}{
		{"invalid name", `{"name": "my flows"}`, nil, http.StatusBadRequest, `"code": "invalid_name"`},
		{"path as name", `{"name": "../x"}`, nil, http.StatusBadRequest, `"code": "invalid_name"`},
		{"unknown field", `{"dir": "/elsewhere"}`, nil, http.StatusBadRequest, "invalid request"},
		{"not JSON", `name=x`, nil, http.StatusBadRequest, "invalid request"},
		{"too large", `{"name": "` + strings.Repeat("a", 5000) + `"}`, nil, http.StatusRequestEntityTooLarge, "too large"},
		{"other origin", `{}`, []string{"Origin", "http://evil.example"}, http.StatusForbidden, "other origins"},
		{"other host", `{}`, []string{"Host", "evil.example:7300"}, http.StatusForbidden, "localhost"},
	} {
		t.Run(c.name, func(t *testing.T) {
			expect(t, doHost(t, h, "POST", "/api/v1/project", c.body, c.header...), c.status, c.want)
		})
	}
	if entries, _ := os.ReadDir(dir); len(entries) > 0 || h.HasProject() {
		t.Errorf("files written: %v", entries)
	}
}

// A project.yaml created in a terminal ("runnel init") is served with the next request.
func TestHostProjectCreatedOutside(t *testing.T) {
	dir, h := newHost(t)
	writeFile(t, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: outside\n")
	expect(t, doHost(t, h, "GET", "/api/v1/project", ""), http.StatusOK, `"name": "outside"`)
	expect(t, doHost(t, h, "GET", "/api/v1/projects/current/tree", ""), http.StatusOK, "project.yaml")
}

func TestHostStopped(t *testing.T) {
	_, h := newHost(t)
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	expect(t, doHost(t, h, "POST", "/api/v1/project", `{}`), http.StatusServiceUnavailable, "shutting down")
}

// BenchmarkHostOverhead compares a request through the host (R10) with one served by
// the project's handler directly.
func BenchmarkHostOverhead(b *testing.B) {
	_, s := newProject(b)
	h, err := Open(Options{Dir: s.dir, Registry: actions.Builtin(), UI: fstest.MapFS{}})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		_ = h.Shutdown(context.Background())
		_ = h.Close()
	})
	for name, handler := range map[string]http.Handler{"server": s.Handler(), "host": h.Handler()} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
				req.Host = "127.0.0.1:7300"
				handler.ServeHTTP(httptest.NewRecorder(), req)
			}
		})
	}
}
