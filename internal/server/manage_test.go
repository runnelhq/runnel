package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	const target = "workflows/import.workflow.yaml"
	if len(res.References) != 2 || res.References[0] != (reference{File: "triggers/nightly.yaml", What: "target", Field: "target", Target: target, Value: target, Line: 5}) ||
		res.References[1] != (reference{File: "workflows/main.workflow.yaml", What: "action sub (core.run-workflow)", Field: "workflow", Target: target, Value: target, Line: 6}) {
		t.Errorf("references %+v", res.References)
	}
	rec = do(t, s, "GET", "/api/v1/references/workflows/Archive.workflow.yaml", "")
	expect(t, rec, http.StatusOK, `"references": []`)
}

// TestRenameReferences moves files and folders and changes the chosen references to the
// new path (E12): only the path in that line, other references stay as they are.
func TestRenameReferences(t *testing.T) {
	dir, s := newProject(t)
	trigger := "kind: Trigger\napiVersion: runnel/v1\nname: nightly\ntype: cron\n# runs the import\ntarget: \"./pipelines/count.pipeline.yaml\"\nwith: { schedule: \"0 2 * * *\" }\n"
	writeFile(t, filepath.Join(dir, "triggers", "nightly", "count.yaml"), trigger)
	main := `kind: Workflow
apiVersion: runnel/v1
name: main
actions:
  - { id: start, type: core.start }
  - { id: count, type: core.run-pipeline, with: { pipeline: pipelines/count.pipeline.yaml } }
  - id: again
    type: core.run-pipeline
    with:
      pipeline: pipelines/count.pipeline.yaml # same pipeline
  - { id: dyn, type: core.run-pipeline, with: { pipeline: "pipelines/${NAME}.pipeline.yaml" } }
links:
  - { from: start, to: count }
  - { from: count, to: again }
  - { from: again, to: dyn }
`
	writeFile(t, filepath.Join(dir, "workflows", "main.workflow.yaml"), main)
	writeFile(t, filepath.Join(dir, "pipelines", "count.pipeline.yaml"), "kind: Pipeline\napiVersion: runnel/v1\nname: count\n")

	var refs struct{ References []reference }
	rec := do(t, s, "GET", "/api/v1/references/pipelines", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &refs); err != nil || len(refs.References) != 3 {
		t.Fatalf("references of the folder: %s %v", rec.Body, err)
	}
	if r := refs.References[0]; r.File != "triggers/nightly/count.yaml" || r.Value != "./pipelines/count.pipeline.yaml" || r.Target != "pipelines/count.pipeline.yaml" || r.Line != 6 {
		t.Errorf("trigger reference %+v", r)
	}

	// The trigger and the first call change, the second call (line 10) is not chosen.
	body := `{"from": "pipelines/count.pipeline.yaml", "to": "shared/count.pipeline.yaml", "references": [
		{"file": "triggers/nightly/count.yaml", "line": 6}, {"file": "workflows/main.workflow.yaml", "line": 6}, {"file": "workflows/main.workflow.yaml", "line": 3}]}`
	rec = do(t, s, "POST", "/api/v1/rename", body)
	var res renameResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s %v", rec.Code, rec.Body, err)
	}
	if len(res.Updated) != 2 || len(res.NotUpdated) != 1 || res.NotUpdated[0].Line != 3 || !strings.Contains(res.NotUpdated[0].Reason, "not found") {
		t.Errorf("response %+v", res)
	}
	if got := readFile(t, filepath.Join(dir, "triggers", "nightly", "count.yaml")); got != strings.Replace(trigger, "./pipelines/", "shared/", 1) {
		t.Errorf("trigger:\n%s", got)
	}
	want := strings.Replace(main, "{ pipeline: pipelines/count", "{ pipeline: shared/count", 1)
	if got := readFile(t, filepath.Join(dir, "workflows", "main.workflow.yaml")); got != want {
		t.Errorf("workflow:\n%s", got)
	}

	// Moving the folder of the referring file: the file is changed at its new place.
	rec = do(t, s, "POST", "/api/v1/rename", `{"from": "workflows", "to": "flows/all", "references": []}`)
	expect(t, rec, http.StatusOK, `"updated": []`)
	rec = do(t, s, "POST", "/api/v1/rename", `{"from": "shared", "to": "lib", "references": [{"file": "flows/all/main.workflow.yaml", "line": 6}]}`)
	expect(t, rec, http.StatusOK, `"flows/all/main.workflow.yaml"`)
	if got := readFile(t, filepath.Join(dir, "flows", "all", "main.workflow.yaml")); !strings.Contains(got, "{ pipeline: lib/count.pipeline.yaml }") ||
		!strings.Contains(got, "pipeline: pipelines/count.pipeline.yaml # same pipeline") {
		t.Errorf("workflow after moving the folder:\n%s", got)
	}

	// A file in another encoding is not changed, and the reason is given.
	utf16 := []byte{0xff, 0xfe}
	for _, r := range "kind: Trigger\napiVersion: runnel/v1\nname: other\ntype: cron\ntarget: lib/count.pipeline.yaml\nwith: { schedule: \"0 2 * * *\" }\n" {
		utf16 = append(utf16, byte(r), 0)
	}
	if err := os.WriteFile(filepath.Join(dir, "triggers", "other.yaml"), utf16, 0o644); err != nil {
		t.Fatal(err)
	}
	rec = do(t, s, "POST", "/api/v1/rename", `{"from": "lib", "to": "shared", "references": [{"file": "triggers/other.yaml", "line": 5}]}`)
	expect(t, rec, http.StatusOK, "only UTF-8 files can be changed")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
