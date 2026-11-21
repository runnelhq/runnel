package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func fieldsBody(t *testing.T, content, transform string) string {
	t.Helper()
	return mustJSON(t, map[string]string{"path": "pipelines/p.pipeline.yaml", "content": content, "transform": transform})
}

// TestDetectFields proposes the fields of csv.input with the unsaved text of the editor,
// with the path resolved from the default of a parameter (S46).
func TestDetectFields(t *testing.T) {
	s := pipelineServer(t)
	file := filepath.Join(s.dir, "data", "in.csv")
	content := strings.Replace(pipelineYAML, "transforms:\n", "parameters:\n  - name: INPUT\n    default: "+mustJSON(t, file)+"\ntransforms:\n", 1)
	content = strings.Replace(content, "path: data/in.csv", "path: ${INPUT}", 1)
	rec := do(t, s, "POST", "/api/v1/pipelines/fields", fieldsBody(t, content, "read"))
	var res struct {
		Source    []struct{ Name, Value string }
		Rows      int
		Complete  bool
		Variables bool
		Fields    []struct {
			Name, Type string
			Values     []string
			Selected   bool
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s %v", rec.Code, rec.Body, err)
	}
	if len(res.Fields) != 2 || res.Fields[0].Name != "id" || res.Fields[0].Type != "int" || res.Fields[1].Type != "string" ||
		!res.Fields[0].Selected || res.Rows != 1 || !res.Complete || !res.Variables || res.Source[0].Value != file {
		t.Errorf("got %+v", res)
	}
}

func TestDetectFieldsRejected(t *testing.T) {
	s := pipelineServer(t)
	missing := strings.Replace(pipelineYAML, "path: data/in.csv", "path: data/missing.csv", 1)
	for _, tt := range []struct {
		body   string
		status int
		want   string
	}{
		{fieldsBody(t, missing, "read"), http.StatusUnprocessableEntity, `"workingDir"`},
		{fieldsBody(t, pipelineYAML, "active"), http.StatusBadRequest, "core.filter cannot detect its fields"},
		{fieldsBody(t, pipelineYAML, "nope"), http.StatusNotFound, "no transform nope"},
		{fieldsBody(t, "kind: [", "read"), http.StatusUnprocessableEntity, "cannot be read"},
		{mustJSON(t, map[string]string{"path": "workflows/import.workflow.yaml", "content": "", "transform": "read"}), http.StatusBadRequest, "pipeline files"},
		{mustJSON(t, map[string]string{"path": "../x.pipeline.yaml", "content": "", "transform": "read"}), http.StatusBadRequest, ""},
		{`{"path": 1}`, http.StatusBadRequest, "invalid request"},
	} {
		expect(t, do(t, s, "POST", "/api/v1/pipelines/fields", tt.body), tt.status, tt.want)
	}
}
