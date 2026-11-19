package server

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/runnelhq/runnel/internal/actions"
)

var update = flag.Bool("update", false, "update golden files")

const workflowYAML = `kind: Workflow
apiVersion: runnel/v1
name: import
actions:
  - { id: start, type: core.start }
  - { id: greet, type: core.log, with: { message: "Hello ${TARGET}" } }
links:
  - { from: start, to: greet }
`

// newProject creates a small project and a server for it.
func newProject(t *testing.T) (string, *Server) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "demo")
	files := map[string]string{
		"project.yaml":                    "kind: Project\napiVersion: runnel/v1\nname: demo\n",
		"environments/dev.yaml":           "kind: Environment\napiVersion: runnel/v1\nname: dev\nvariables:\n  TARGET: dev\n",
		"workflows/import.workflow.yaml":  workflowYAML,
		"workflows/Archive.workflow.yaml": workflowYAML,
		"data/in.csv":                     "id;name\n1;a\n",
		".git/config":                     "hidden",
		".env":                            "hidden",
	}
	for name, content := range files {
		writeFile(t, filepath.Join(dir, name), content)
	}
	// An empty UI: tests must not depend on whether the editor was built.
	s, err := New(Options{Dir: dir, Registry: actions.Builtin(), UI: fstest.MapFS{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = s.Shutdown(context.Background())
		_ = s.Close()
	})
	return dir, s
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// do sends a request as a local client would.
func do(t *testing.T, s *Server, method, target, body string, header ...string) *httptest.ResponseRecorder {
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
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func expect(t *testing.T, rec *httptest.ResponseRecorder, status int, bodyContains string) {
	t.Helper()
	if rec.Code != status || !strings.Contains(rec.Body.String(), bodyContains) {
		t.Errorf("status %d, body %q; want %d containing %q", rec.Code, rec.Body.String(), status, bodyContains)
	}
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden file (run with -update): %v", err)
	}
	if !bytes.Equal(got, bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))) {
		t.Errorf("response differs from %s (run with -update if intended)\n--- got:\n%s", path, got)
	}
}

func TestGuard(t *testing.T) {
	_, s := newProject(t)
	expect(t, do(t, s, "GET", "/api/v1/catalog", "", "Host", "evil.example:7300"), http.StatusForbidden, "localhost or 127.0.0.1")
	expect(t, do(t, s, "GET", "/api/v1/catalog", "", "Origin", "http://evil.example"), http.StatusForbidden, "other origins")
	expect(t, do(t, s, "GET", "/api/v1/catalog", "", "Origin", "http://127.0.0.1:7300"), http.StatusOK, "core.log")
	expect(t, do(t, s, "GET", "/api/v1/catalog", "", "Host", "localhost:7300"), http.StatusOK, "core.log")
	expect(t, do(t, s, "GET", "/api/v1/catalog", "", "Host", "[::1]:7300"), http.StatusOK, "core.log")
}

func TestUnknownEndpoint(t *testing.T) {
	_, s := newProject(t)
	expect(t, do(t, s, "GET", "/api/v1/nothing", ""), http.StatusNotFound, "unknown endpoint GET /api/v1/nothing")
	expect(t, do(t, s, "GET", "/", ""), http.StatusOK, "does not include the editor")
}

func TestProject(t *testing.T) {
	dir, s := newProject(t)
	rec := do(t, s, "GET", "/api/v1/projects/current", "")
	var info projectInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil || info.Name != "demo" || info.Dir != dir {
		t.Errorf("project %+v, err %v", info, err)
	}
}

func TestTree(t *testing.T) {
	dir, s := newProject(t)
	if runtime.GOOS != "windows" { // symbolic links need extra rights on Windows
		if err := os.Symlink(filepath.Join(dir, "data"), filepath.Join(dir, "link")); err != nil {
			t.Fatal(err)
		}
	}
	rec := do(t, s, "GET", "/api/v1/projects/current/tree", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	golden(t, "tree.golden.json", rec.Body.Bytes())
}

func TestGetFile(t *testing.T) {
	_, s := newProject(t)
	rec := do(t, s, "GET", "/api/v1/files/workflows/import.workflow.yaml", "")
	expect(t, rec, http.StatusOK, "name: import")
	if rec.Header().Get("ETag") != etag([]byte(workflowYAML)) || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/yaml") {
		t.Errorf("headers %v", rec.Header())
	}
	expect(t, do(t, s, "GET", "/api/v1/files/workflows/missing.workflow.yaml", ""), http.StatusNotFound, "file not found")
	expect(t, do(t, s, "GET", "/api/v1/files/workflows", ""), http.StatusBadRequest, "is a directory")
	expect(t, do(t, s, "GET", "/api/v1/files/.git/config", ""), http.StatusBadRequest, "hidden")
}

func TestGetFileOutsideProject(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need extra rights on Windows")
	}
	dir, s := newProject(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	writeFile(t, outside, "secret")
	if err := os.Symlink(outside, filepath.Join(dir, "secret.txt")); err != nil {
		t.Fatal(err)
	}
	if rec := do(t, s, "GET", "/api/v1/files/secret.txt", ""); rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "secret\"") {
		t.Errorf("file outside the project was served: %d %s", rec.Code, rec.Body)
	}
}

func TestPutFile(t *testing.T) {
	dir, s := newProject(t)
	const path = "/api/v1/files/workflows/import.workflow.yaml"
	oldTag := etag([]byte(workflowYAML))
	changed := strings.Replace(workflowYAML, "Hello", "Hi", 1)

	expect(t, do(t, s, "PUT", path, changed), http.StatusPreconditionRequired, "If-Match")
	expect(t, do(t, s, "PUT", path, changed, "If-Match", `"0000"`), http.StatusPreconditionFailed, "changed in the meantime")
	rec := do(t, s, "PUT", path, changed, "If-Match", oldTag)
	expect(t, rec, http.StatusOK, `"path": "workflows/import.workflow.yaml"`)
	if rec.Header().Get("ETag") != etag([]byte(changed)) {
		t.Errorf("ETag %q", rec.Header().Get("ETag"))
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "workflows", "import.workflow.yaml")); string(data) != changed {
		t.Errorf("file content %q", data)
	}
	// A second save with the old ETag (e.g. from another browser) is rejected.
	expect(t, do(t, s, "PUT", path, workflowYAML, "If-Match", oldTag), http.StatusPreconditionFailed, "changed in the meantime")

	// Creating a file, including missing directories.
	expect(t, do(t, s, "PUT", "/api/v1/files/new/sub/x.workflow.yaml", "x"), http.StatusCreated, `"path": "new/sub/x.workflow.yaml"`)
	expect(t, do(t, s, "PUT", "/api/v1/files/gone.workflow.yaml", "x", "If-Match", oldTag), http.StatusPreconditionFailed, "deleted in the meantime")
	expect(t, do(t, s, "PUT", "/api/v1/files/big.csv", strings.Repeat("x", MaxFileSize+1)), http.StatusRequestEntityTooLarge, "10 MiB")
	expect(t, do(t, s, "PUT", "/api/v1/files/a%3Ab.yaml", "x"), http.StatusBadRequest, "must not contain")

	// No temporary files are left behind.
	entries, _ := os.ReadDir(filepath.Join(dir, "workflows"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".runnel-") {
			t.Errorf("temporary file left: %s", e.Name())
		}
	}
}

func TestDeleteFile(t *testing.T) {
	dir, s := newProject(t)
	const path = "/api/v1/files/data/in.csv"
	tag := etag([]byte("id;name\n1;a\n"))
	expect(t, do(t, s, "DELETE", path, ""), http.StatusPreconditionRequired, "If-Match")
	expect(t, do(t, s, "DELETE", path, "", "If-Match", `"0000"`), http.StatusPreconditionFailed, "changed in the meantime")
	if rec := do(t, s, "DELETE", path, "", "If-Match", tag); rec.Code != http.StatusNoContent {
		t.Errorf("status %d: %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "in.csv")); !os.IsNotExist(err) {
		t.Errorf("file still exists: %v", err)
	}
	expect(t, do(t, s, "DELETE", path, "", "If-Match", tag), http.StatusPreconditionFailed, "deleted in the meantime")
}

func TestFilePath(t *testing.T) {
	valid := []string{"workflows/a.workflow.yaml", "data/Kunden 2026.csv", "Ä/ü.yaml"}
	for _, p := range valid {
		if _, err := filePath(p); err != nil {
			t.Errorf("%q: %v", p, err)
		}
	}
	invalid := map[string]string{
		"":                 "missing file path",
		`workflows\a.yaml`: `use "/" as separator`,
		"a/../b":           `"." or ".."`,
		"a//b":             `"." or ".."`,
		".git/config":      "hidden",
		"C:/x":             "must not contain",
		"a/b?.yaml":        "must not contain",
		"a/b. ":            "end with a space or a dot",
		"nul.txt":          "reserved Windows device name",
		"COM1":             "reserved Windows device name",
		"lpt9.csv":         "reserved Windows device name",
	}
	for p, want := range invalid {
		if _, err := filePath(p); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v, want containing %q", p, err, want)
		}
	}
	for _, p := range []string{"COM", "COMX", "console.yaml", "nullable.yaml"} {
		if isReservedName(p) {
			t.Errorf("%q is not reserved", p)
		}
	}
}

func TestCatalog(t *testing.T) {
	_, s := newProject(t)
	var c Catalog
	rec := do(t, s, "GET", "/api/v1/catalog", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil || len(c.Actions) == 0 || c.Actions[0].Schema == nil {
		t.Errorf("catalog %s, err %v", rec.Body, err)
	}
}

func TestValidate(t *testing.T) {
	_, s := newProject(t)
	tests := []struct {
		golden string
		body   string
	}{
		{"validate-saved.golden.json", `{"path": "workflows/import.workflow.yaml"}`},
		{"validate-content.golden.json", mustJSON(t, map[string]string{"path": "workflows/import.workflow.yaml", "content": `kind: Workflow
apiVersion: runnel/v1
name: import
actions:
  - { id: start, type: core.start }
  - { id: copy, type: file.copy }
  - { id: greet, type: core.log, with: { msg: "${MISSING}" } }
  - { id: lonely, type: core.dummy }
links:
  - { from: start, to: greet }
  - { from: greet, to: copy }
`})},
		{"validate-syntax.golden.json", mustJSON(t, map[string]string{"path": "workflows/new.workflow.yaml", "content": "kind: Workflow\nactions: [\n"})},
	}
	for _, tt := range tests {
		t.Run(tt.golden, func(t *testing.T) {
			rec := do(t, s, "POST", "/api/v1/validate", tt.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			golden(t, tt.golden, rec.Body.Bytes())
		})
	}
}

func TestValidateWithInvalidProject(t *testing.T) {
	dir, s := newProject(t)
	writeFile(t, filepath.Join(dir, "environments", "dev.yaml"), "kind: Environment\nname: dev\nvariables: [1]\n")
	rec := do(t, s, "POST", "/api/v1/validate", `{"path": "workflows/import.workflow.yaml"}`)
	expect(t, rec, http.StatusOK, `"file": "environments/dev.yaml"`)
}

func TestValidateBadRequests(t *testing.T) {
	_, s := newProject(t)
	expect(t, do(t, s, "POST", "/api/v1/validate", `{"path": "data/in.csv"}`), http.StatusBadRequest, "only workflow files")
	expect(t, do(t, s, "POST", "/api/v1/validate", `{"file": "x"}`), http.StatusBadRequest, "invalid request")
	expect(t, do(t, s, "POST", "/api/v1/validate", `{"path": "../x.workflow.yaml"}`), http.StatusBadRequest, "invalid path")
	expect(t, do(t, s, "POST", "/api/v1/validate", `{"path": "workflows/none.workflow.yaml"}`), http.StatusNotFound, "file not found")
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestEditWorkflow(t *testing.T) {
	_, s := newProject(t)
	body := mustJSON(t, map[string]any{
		"path":    "workflows/import.workflow.yaml",
		"content": workflowYAML,
		"move":    map[string]any{"greet": map[string]int{"x": 280, "y": 40}},
	})
	rec := do(t, s, "POST", "/api/v1/workflows/edit", body)
	var res struct{ Content string }
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if want := workflowYAML + "layout:\n  greet: { x: 280, y: 40 }\n"; res.Content != want {
		t.Errorf("content:\n%s\nwant:\n%s", res.Content, want)
	}

	body = mustJSON(t, map[string]any{"path": "workflows/import.workflow.yaml", "content": workflowYAML, "delete": []string{"greet"}})
	rec = do(t, s, "POST", "/api/v1/workflows/edit", body)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "greet") {
		t.Errorf("delete: status %d: %s", rec.Code, rec.Body)
	}

	expect(t, do(t, s, "POST", "/api/v1/workflows/edit", mustJSON(t, map[string]any{"path": "workflows/import.workflow.yaml", "content": workflowYAML, "delete": []string{"nope"}})),
		http.StatusBadRequest, `cannot delete action \"nope\"`)
	expect(t, do(t, s, "POST", "/api/v1/workflows/edit", mustJSON(t, map[string]any{"path": "workflows/x.workflow.yaml", "content": "kind: [\n"})),
		http.StatusUnprocessableEntity, "prevent editing")
	expect(t, do(t, s, "POST", "/api/v1/workflows/edit", mustJSON(t, map[string]any{"path": "data/in.csv", "content": ""})),
		http.StatusBadRequest, "only workflow files")
}
