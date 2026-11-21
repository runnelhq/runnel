package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestFolders creates and deletes folders (E11); only empty ones can be deleted.
func TestFolders(t *testing.T) {
	dir, s := newProject(t)
	expect(t, do(t, s, "POST", "/api/v1/folders", `{"path": "data/archive"}`), http.StatusCreated, `"data/archive"`)
	if info, err := os.Stat(filepath.Join(dir, "data", "archive")); err != nil || !info.IsDir() {
		t.Fatalf("folder not created: %v", err)
	}
	expect(t, do(t, s, "POST", "/api/v1/folders", `{"path": "data/archive"}`), http.StatusConflict, "already exists")
	expect(t, do(t, s, "POST", "/api/v1/folders", `{"path": ".hidden"}`), http.StatusBadRequest, "hidden")
	expect(t, do(t, s, "POST", "/api/v1/folders", `{"path": "../out"}`), http.StatusBadRequest, "invalid path")

	expect(t, do(t, s, "DELETE", "/api/v1/folders/data", ""), http.StatusConflict, "only empty folders")
	writeFile(t, filepath.Join(dir, "data", "archive", ".keep"), "")
	expect(t, do(t, s, "DELETE", "/api/v1/folders/data/archive", ""), http.StatusConflict, "hidden files")
	_ = os.Remove(filepath.Join(dir, "data", "archive", ".keep"))
	expect(t, do(t, s, "DELETE", "/api/v1/folders/data/archive", ""), http.StatusNoContent, "")
	expect(t, do(t, s, "DELETE", "/api/v1/folders/data/archive", ""), http.StatusNotFound, "folder not found")
	expect(t, do(t, s, "DELETE", "/api/v1/folders/data/in.csv", ""), http.StatusBadRequest, "is a file")
}

// TestRename renames and moves files and folders within the project.
func TestRename(t *testing.T) {
	dir, s := newProject(t)
	expect(t, do(t, s, "POST", "/api/v1/rename", `{"from": "workflows/import.workflow.yaml", "to": "workflows/load.workflow.yaml"}`), http.StatusOK, `"workflows/load.workflow.yaml"`)
	if _, err := os.Stat(filepath.Join(dir, "workflows", "load.workflow.yaml")); err != nil {
		t.Fatal(err)
	}
	expect(t, do(t, s, "POST", "/api/v1/rename", `{"from": "data", "to": "input/data"}`), http.StatusOK, "")
	if _, err := os.Stat(filepath.Join(dir, "input", "data", "in.csv")); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		body   string
		status int
		want   string
	}{
		{`{"from": "workflows/load.workflow.yaml", "to": "workflows/Archive.workflow.yaml"}`, http.StatusConflict, "already exists"},
		{`{"from": "input", "to": "input/inner"}`, http.StatusBadRequest, "into itself"},
		{`{"from": "input", "to": "input"}`, http.StatusBadRequest, "the old one"},
		{`{"from": "none.yaml", "to": "x.yaml"}`, http.StatusNotFound, "file not found"},
		{`{"from": "input", "to": "../x"}`, http.StatusBadRequest, "invalid path"},
		{`{"from": "input"}`, http.StatusBadRequest, "missing file path"},
	} {
		expect(t, do(t, s, "POST", "/api/v1/rename", tt.body), tt.status, tt.want)
	}
}

// TestReferences lists triggers and workflow calls that refer to a workflow.
func TestReferences(t *testing.T) {
	dir, s := newProject(t)
	writeFile(t, filepath.Join(dir, "triggers", "nightly.yaml"),
		"kind: Trigger\napiVersion: runnel/v1\nname: nightly\ntype: cron\ntarget: workflows/import.workflow.yaml\nwith: { schedule: \"0 2 * * *\" }\n")
	writeFile(t, filepath.Join(dir, "workflows", "main.workflow.yaml"), `kind: Workflow
apiVersion: runnel/v1
name: main
actions:
  - { id: start, type: core.start }
  - { id: sub, type: core.run-workflow, with: { workflow: workflows/import.workflow.yaml } }
links:
  - { from: start, to: sub }
`)
	rec := do(t, s, "GET", "/api/v1/references/workflows/import.workflow.yaml", "")
	var res struct{ References []reference }
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s %v", rec.Code, rec.Body, err)
	}
	if len(res.References) != 2 || res.References[0] != (reference{File: "triggers/nightly.yaml", What: "target"}) ||
		res.References[1] != (reference{File: "workflows/main.workflow.yaml", What: "action sub (core.run-workflow)"}) {
		t.Errorf("references %+v", res.References)
	}
	rec = do(t, s, "GET", "/api/v1/references/workflows/Archive.workflow.yaml", "")
	expect(t, rec, http.StatusOK, `"references": []`)
}
