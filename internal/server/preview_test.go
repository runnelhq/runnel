package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

const previewYAML = `kind: Pipeline
apiVersion: runnel/v1
name: customers
parameters:
  - name: INPUT
transforms:
  - id: read
    type: csv.input
    with: { path: "${INPUT}", delimiter: ";" }
  - id: types
    type: core.convert
    with:
      fields:
        - { name: id, type: int }
  - id: rejected
    type: csv.output
    with: { path: rejected.csv }
  - id: write
    type: csv.output
    with: { path: out.csv }
links:
  - { from: read, to: types }
  - { from: types, to: write }
  - { from: types, to: rejected, type: error }
`

func previewBody(t *testing.T, content string, params map[string]string, limit int) string {
	t.Helper()
	return mustJSON(t, map[string]any{"path": "pipelines/p.pipeline.yaml", "content": content, "transform": "types", "limit": limit, "params": params})
}

// TestPreview runs the text of the editor up to "types" with the first records (S44).
func TestPreview(t *testing.T) {
	s := pipelineServer(t)
	file := filepath.Join(s.dir, "data", "preview.csv")
	writeFile(t, file, "id;name\n1;a\nx;b\n3;c\n4;d\n")
	rec := do(t, s, "POST", "/api/v1/preview", previewBody(t, previewYAML, map[string]string{"INPUT": file}, 3))
	var res previewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s %v", rec.Code, rec.Body, err)
	}
	if res.Status != "succeeded" || res.Limit != 3 || res.Counts["read"].Written != 3 || res.Counts["types"].Errors != 1 {
		t.Errorf("status %s, limit %d, counts %+v", res.Status, res.Limit, res.Counts)
	}
	out, _ := json.Marshal(res.Output)
	if string(out) != `{"rows":2,"fields":[{"name":"id","type":"int"},{"name":"name","type":"string"}],"records":[[1,"a"],[3,"c"]]}` {
		t.Errorf("output %s", out)
	}
	errLink := res.Links[2]
	if errLink.Rows != 1 || len(errLink.Fields) != 5 || errLink.Fields[2].Name != "_error" || errLink.Records[0][0] != "x" {
		t.Errorf("error link %+v", errLink)
	}
	if _, ok := res.Counts["write"]; ok || len(res.Counts) != 2 {
		t.Errorf("counts of transforms after types: %v", res.Counts)
	}
	if res.Links[1].Rows != 2 {
		t.Errorf("link to write: %+v", res.Links[1])
	}
	// Nothing was written: the outputs are left out.
	if files, _ := filepath.Glob(filepath.Join(s.dir, "*.csv")); len(files) > 0 {
		t.Errorf("written: %v", files)
	}
}

func TestPreviewRejected(t *testing.T) {
	s := pipelineServer(t)
	missing := map[string]string{"INPUT": filepath.Join(s.dir, "data", "missing.csv")}
	rec := do(t, s, "POST", "/api/v1/preview", previewBody(t, previewYAML, missing, 0))
	var res previewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s %v", rec.Code, rec.Body, err)
	}
	if res.Status != "failed" || res.FailedAt != "read" || !strings.Contains(res.Error, "missing.csv") || res.Limit != 100 {
		t.Errorf("failed preview: %+v", res)
	}
	for _, tt := range []struct {
		body   string
		status int
		want   string
	}{
		{mustJSON(t, map[string]any{"path": "pipelines/p.pipeline.yaml", "content": previewYAML, "transform": "nope", "params": missing}), http.StatusUnprocessableEntity, `the pipeline has no transform`},
		{previewBody(t, "kind: Pipeline\napiVersion: runnel/v1\nname: x\ntransforms: [\n", nil, 0), http.StatusUnprocessableEntity, "problems"},
		{mustJSON(t, map[string]any{"path": "workflows/import.workflow.yaml", "content": "", "transform": "a"}), http.StatusBadRequest, "only pipelines"},
		{`{"path": 1}`, http.StatusBadRequest, "invalid request"},
	} {
		expect(t, do(t, s, "POST", "/api/v1/preview", tt.body), tt.status, tt.want)
	}
}
