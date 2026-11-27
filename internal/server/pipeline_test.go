package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/engine/pipeline"
	"github.com/runnelhq/runnel/internal/runs"
	"github.com/runnelhq/runnel/internal/transforms"
)

const pipelineYAML = `kind: Pipeline
apiVersion: runnel/v1
name: customers
transforms:
  - id: read
    type: csv.input
    with:
      path: data/in.csv
      delimiter: ";"
  - id: active
    type: core.filter
    with:
      condition: "{{ record.umsatzz > 0 }}"
links:
  - { from: read, to: active }
layout:
  read: { x: 16, y: 130 }
`

// pipelineServer is a project with a pipeline and the built-in transforms.
func pipelineServer(t *testing.T) *Server {
	t.Helper()
	dir, _ := newProject(t)
	writeFile(t, filepath.Join(dir, "pipelines", "customers.pipeline.yaml"), pipelineYAML)
	s, err := New(Options{Dir: dir, Registry: actions.Builtin(), Transforms: transforms.Builtin(), UI: fstest.MapFS{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = s.Shutdown(context.Background())
		_ = s.Close()
	})
	return s
}

func TestValidatePipeline(t *testing.T) {
	s := pipelineServer(t)
	rec := do(t, s, "POST", "/api/v1/validate", `{"path": "pipelines/customers.pipeline.yaml"}`)
	var res validateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s %v", rec.Code, rec.Body, err)
	}
	if res.Pipeline == nil || len(res.Pipeline.Transforms) != 2 || res.Pipeline.Layout["read"].X != 16 || res.Workflow != nil {
		t.Errorf("pipeline %+v", res.Pipeline)
	}
	// The schemas around the transforms: csv.input without fields reads an open schema.
	if sc := res.Schemas["active"]; sc.Input == nil || !sc.Input.Open || res.Schemas["read"].Input != nil || len(res.Schemas) != 2 {
		t.Errorf("schemas %+v", res.Schemas)
	}
	// An open schema (csv.input without fields) gives no warning for umsatzz; an unknown
	// field of a transform is an error at its place.
	content := strings.Replace(pipelineYAML, "      delimiter: \";\"\n", "      delimiter: \";\"\n      sheet: 1\n", 1)
	rec = do(t, s, "POST", "/api/v1/validate", mustJSON(t, map[string]any{"path": "pipelines/customers.pipeline.yaml", "content": content}))
	res = validateResponse{}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Schemas["active"].Input == nil {
		t.Errorf("schemas missing despite an error in a transform: %s", rec.Body)
	}
	if res.Errors != 1 || res.Problems[0].Path != "$.transforms[0].with" || !strings.Contains(res.Problems[0].Message, `unknown field "sheet"`) {
		t.Errorf("problems %+v", res.Problems)
	}
	// A file that cannot be read as pipeline: problems, no pipeline.
	rec = do(t, s, "POST", "/api/v1/validate", mustJSON(t, map[string]any{"path": "pipelines/customers.pipeline.yaml", "content": "kind: [\n"}))
	res = validateResponse{}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || res.Pipeline != nil || res.Errors == 0 {
		t.Errorf("%s %v", rec.Body, err)
	}
}

func TestEditPipeline(t *testing.T) {
	s := pipelineServer(t)
	req := map[string]any{"path": "pipelines/customers.pipeline.yaml", "content": pipelineYAML,
		"add": []any{map[string]any{"id": "rejected", "type": "csv.output"}}, "move": map[string]any{"rejected": map[string]any{"x": 256, "y": 380}},
		"addLinks": []any{map[string]any{"from": "read", "to": "rejected", "type": "error"}}}
	rec := do(t, s, "POST", "/api/v1/pipelines/edit", mustJSON(t, req))
	var res editResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s %v", rec.Code, rec.Body, err)
	}
	for _, want := range []string{"  - id: rejected\n    type: csv.output\n", "  - { from: read, to: rejected, type: error }\n", "  rejected: { x: 256, y: 380 }\n"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("missing %q in:\n%s", want, res.Content)
		}
	}
	expect(t, do(t, s, "POST", "/api/v1/pipelines/edit", mustJSON(t, map[string]any{"path": "pipelines/customers.pipeline.yaml", "content": pipelineYAML, "delete": []string{"nope"}})),
		http.StatusBadRequest, `cannot delete transform \"nope\"`)
	expect(t, do(t, s, "POST", "/api/v1/pipelines/edit", mustJSON(t, map[string]any{"path": "pipelines/customers.pipeline.yaml", "content": "kind: [\n"})),
		http.StatusUnprocessableEntity, "prevent editing")
	expect(t, do(t, s, "POST", "/api/v1/pipelines/edit", mustJSON(t, map[string]any{"path": "workflows/import.workflow.yaml", "content": ""})),
		http.StatusBadRequest, "only pipeline files")
}

// A pipeline runs like a workflow; its transforms are reported as actions, with counts (S25a3).
func TestRunPipeline(t *testing.T) {
	s := pipelineServer(t)
	writeFile(t, filepath.Join(s.dir, "pipelines", "numbers.pipeline.yaml"), `kind: Pipeline
apiVersion: runnel/v1
name: numbers
transforms:
  - { id: gen, type: core.generate-rows, with: { count: 10, numberField: n } }
  - id: even
    type: core.filter
    with: { condition: "{{ record.n % 2 == 0 }}" }
  - { id: out, type: core.dummy }
links:
  - { from: gen, to: even }
  - { from: even, to: out }
`)
	run := startRun(t, s, `{"path": "pipelines/numbers.pipeline.yaml"}`)
	if run.Kind != "pipeline" || run.Name != "numbers" || len(run.LinkRows) != 2 {
		t.Errorf("run %+v", run)
	}
	d := waitRun(t, s, run.ID, func(d runs.Detail) bool { return d.Status != runs.StatusRunning })
	if d.Status != "succeeded" || len(d.Actions) != 3 || d.Actions[1].Counts == nil || *d.Actions[1].Counts != (pipeline.Counts{Read: 10, Written: 5}) {
		t.Fatalf("run %+v, actions %+v", d.Run, d.Actions)
	}
	if fmt.Sprint(d.LinkRows) != "[10 5]" {
		t.Errorf("link rows %v", d.LinkRows)
	}
	// A pipeline with problems is not started.
	writeFile(t, filepath.Join(s.dir, "pipelines", "broken.pipeline.yaml"), "kind: Pipeline\napiVersion: runnel/v1\nname: broken\ntransforms:\n  - { id: x, type: core.nope }\n")
	rec := do(t, s, "POST", "/api/v1/runs", `{"path": "pipelines/broken.pipeline.yaml"}`)
	expect(t, rec, http.StatusUnprocessableEntity, "the pipeline cannot be started")
}

// Renaming a transform on the canvas also changes the fields of other transforms that
// name it, taken from their schemas (format runnel-target).
func TestEditPipelineRenameTargets(t *testing.T) {
	s := pipelineServer(t)
	content := `kind: Pipeline
apiVersion: runnel/v1
name: route
transforms:
  - { id: gen, type: core.generate-rows, with: { count: 1 } }
  - id: check
    type: core.filter
    with:
      condition: "{{ true }}"
      else: rejected
  - { id: ok, type: core.dummy }
  - { id: rejected, type: core.dummy }
links:
  - { from: gen, to: check }
  - { from: check, to: ok }
  - { from: check, to: rejected }
`
	rec := do(t, s, "POST", "/api/v1/pipelines/edit", mustJSON(t, map[string]any{"path": "pipelines/route.pipeline.yaml", "content": content,
		"rename": map[string]string{"rejected": "invalid"}}))
	var res editResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s %v", rec.Code, rec.Body, err)
	}
	for _, want := range []string{"      else: invalid\n", "  - { id: invalid, type: core.dummy }\n", "  - { from: check, to: invalid }\n"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("missing %q in:\n%s", want, res.Content)
		}
	}
}
