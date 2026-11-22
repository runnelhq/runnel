package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/model"
)

// TestCopyAndPaste: copied actions pasted into the same workflow get new ids (E16).
func TestCopyAndPaste(t *testing.T) {
	_, s := newProject(t)
	rec := do(t, s, "POST", "/api/v1/copy", mustJSON(t, map[string]any{
		"path": "workflows/import.workflow.yaml", "content": workflowYAML, "ids": []string{"start", "greet"}}))
	var clip model.Clip
	if err := json.Unmarshal(rec.Body.Bytes(), &clip); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("copy: status %d: %s", rec.Code, rec.Body)
	}
	if clip.Steps != 2 || clip.Links != 1 || !strings.Contains(clip.Text, "  - { id: greet, type: core.log") {
		t.Errorf("copy: %+v", clip)
	}

	rec = do(t, s, "POST", "/api/v1/workflows/edit", mustJSON(t, map[string]any{
		"path": "workflows/import.workflow.yaml", "content": workflowYAML, "paste": map[string]any{"text": clip.Text}}))
	var res editResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("paste: status %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(res.Content, "  - { id: greet-2, type: core.log") || !strings.Contains(res.Content, "  - { from: start-2, to: greet-2 }") {
		t.Errorf("paste:\n%s", res.Content)
	}
	if res.Pasted == nil || strings.Join(res.Pasted.IDs, ",") != "start-2,greet-2" || res.Pasted.Links != 1 {
		t.Errorf("pasted = %+v", res.Pasted)
	}
}

// TestPastePipelineAPI: transforms are pasted through the edit API of pipelines.
func TestPastePipelineAPI(t *testing.T) {
	s := pipelineServer(t)
	rec := do(t, s, "POST", "/api/v1/copy", mustJSON(t, map[string]any{
		"path": "pipelines/customers.pipeline.yaml", "content": pipelineYAML, "ids": []string{"active"}}))
	var clip model.Clip
	if err := json.Unmarshal(rec.Body.Bytes(), &clip); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("copy: status %d: %s", rec.Code, rec.Body)
	}
	if clip.Omitted != 1 {
		t.Errorf("omitted = %d, want 1", clip.Omitted)
	}
	rec = do(t, s, "POST", "/api/v1/pipelines/edit", mustJSON(t, map[string]any{
		"path": "pipelines/customers.pipeline.yaml", "content": pipelineYAML,
		"paste": map[string]any{"text": clip.Text, "at": map[string]any{"x": 100, "y": 200}}}))
	var res editResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("paste: status %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(res.Content, "  - id: active-2\n    type: core.filter\n") {
		t.Errorf("paste:\n%s", res.Content)
	}
}

// TestCopyPasteRejected: unknown ids, other files, the wrong kind and oversized text.
func TestCopyPasteRejected(t *testing.T) {
	_, s := newProject(t)
	expect(t, do(t, s, "POST", "/api/v1/copy", mustJSON(t, map[string]any{
		"path": "workflows/import.workflow.yaml", "content": workflowYAML, "ids": []string{"nope"}})),
		http.StatusBadRequest, `cannot copy action \"nope\"`)
	expect(t, do(t, s, "POST", "/api/v1/copy", mustJSON(t, map[string]any{
		"path": "project.yaml", "content": "kind: Project\n", "ids": []string{"a"}})),
		http.StatusBadRequest, "only actions of workflows and transforms of pipelines")
	expect(t, do(t, s, "POST", "/api/v1/copy", mustJSON(t, map[string]any{
		"path": "../x.workflow.yaml", "content": workflowYAML, "ids": []string{"start"}})),
		http.StatusBadRequest, "")
	expect(t, do(t, s, "POST", "/api/v1/workflows/edit", mustJSON(t, map[string]any{
		"path": "workflows/import.workflow.yaml", "content": workflowYAML,
		"paste": map[string]any{"text": "transforms:\n  - id: x\n    type: core.dummy\n"}})),
		http.StatusBadRequest, "Transforms can't be pasted into a workflow")
	expect(t, do(t, s, "POST", "/api/v1/workflows/edit", mustJSON(t, map[string]any{
		"path": "workflows/import.workflow.yaml", "content": workflowYAML,
		"paste": map[string]any{"text": strings.Repeat("x", model.MaxPasteSize+1)}})),
		http.StatusBadRequest, "too large")
	big := strings.Repeat("x", MaxFileSize+model.MaxPasteSize+64*1024)
	rec := do(t, s, "POST", "/api/v1/workflows/edit", mustJSON(t, map[string]any{
		"path": "workflows/import.workflow.yaml", "content": big, "paste": map[string]any{"text": "x"}}))
	if rec.Code < 400 {
		t.Errorf("oversized request: status %d", rec.Code)
	}
}
