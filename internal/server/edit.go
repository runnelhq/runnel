package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/runs"
	"github.com/runnelhq/runnel/internal/schema"
)

// editRequest applies changes made on the canvas to the text the editor shows.
type editRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	// Paste appends copied actions (E16); it cannot be combined with other changes.
	Paste *model.Paste `json:"paste,omitempty"`
	model.WorkflowEdit
}

type editResponse struct {
	Content string `json:"content"`
	// Pasted tells what was pasted (E16).
	Pasted *model.Pasted `json:"pasted,omitempty"`
}

// handleEditWorkflow changes the text of a workflow (move, delete) and returns the new
// text; it does not save. Comments and formatting of untouched lines stay as they are.
func (s *Server) handleEditWorkflow(w http.ResponseWriter, r *http.Request) {
	var req editRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxFileSize+model.MaxPasteSize+64*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	name, err := filePath(req.Path)
	if err != nil {
		writeErr(w, err)
		return
	}
	if fileKind(name) != "workflow" {
		writeError(w, http.StatusBadRequest, "only workflow files (*.workflow.yaml) can be edited on the canvas")
		return
	}
	if req.Paste != nil {
		out, pasted, err := model.PasteWorkflow(name, []byte(req.Content), *req.Paste)
		writeEdit(w, s, "workflow", out, &pasted, err)
		return
	}
	out, err := model.EditWorkflow(name, []byte(req.Content), req.WorkflowEdit)
	var diags model.Errors
	switch {
	case errors.As(err, &diags):
		writeJSON(w, http.StatusUnprocessableEntity, startProblems{
			Error:            "the workflow has errors that prevent editing it on the canvas",
			validateResponse: s.response(diags),
		})
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeJSON(w, http.StatusOK, editResponse{Content: string(out)})
	}
}

// pipelineEditRequest applies changes made on the canvas of a pipeline (S25a).
type pipelineEditRequest struct {
	Path    string       `json:"path"`
	Content string       `json:"content"`
	Paste   *model.Paste `json:"paste,omitempty"`
	model.PipelineEdit
}

// handleEditPipeline changes the text of a pipeline like handleEditWorkflow; it does not save.
func (s *Server) handleEditPipeline(w http.ResponseWriter, r *http.Request) {
	var req pipelineEditRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxFileSize+model.MaxPasteSize+64*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	name, err := filePath(req.Path)
	if err != nil {
		writeErr(w, err)
		return
	}
	if fileKind(name) != "pipeline" {
		writeError(w, http.StatusBadRequest, "only pipeline files (*.pipeline.yaml) can be edited here")
		return
	}
	if req.Paste != nil {
		out, pasted, err := model.PastePipeline(name, []byte(req.Content), *req.Paste)
		writeEdit(w, s, "pipeline", out, &pasted, err)
		return
	}
	if len(req.Rename) > 0 {
		// Renaming also changes the fields that name transforms (to, default, else …).
		reg := runs.WithPluginTransforms(s.transforms, s.dir)
		req.Targets = func(typ string) [][]string {
			if d, ok := reg.Descriptor(typ); ok {
				return d.Schema.Paths(schema.FormatTarget)
			}
			return nil
		}
	}
	out, err := model.EditPipeline(name, []byte(req.Content), req.PipelineEdit)
	var diags model.Errors
	switch {
	case errors.As(err, &diags):
		writeJSON(w, http.StatusUnprocessableEntity, startProblems{
			Error:            "the pipeline has errors that prevent editing it on the canvas",
			validateResponse: s.response(diags),
		})
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeJSON(w, http.StatusOK, editResponse{Content: string(out)})
	}
}

// writeEdit answers a paste (E16) like the other edits.
func writeEdit(w http.ResponseWriter, s *Server, noun string, out []byte, pasted *model.Pasted, err error) {
	var diags model.Errors
	switch {
	case errors.As(err, &diags):
		writeJSON(w, http.StatusUnprocessableEntity, startProblems{
			Error:            "the " + noun + " has errors that prevent editing it on the canvas",
			validateResponse: s.response(diags),
		})
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeJSON(w, http.StatusOK, editResponse{Content: string(out), Pasted: pasted})
	}
}

// copyRequest copies actions or transforms with the links between them (E16).
type copyRequest struct {
	Path    string   `json:"path"`
	Content string   `json:"content"`
	IDs     []string `json:"ids"`
}

// handleCopy returns the selected actions or transforms of the text the editor shows as
// YAML text for the clipboard.
func (s *Server) handleCopy(w http.ResponseWriter, r *http.Request) {
	var req copyRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxFileSize+64*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	name, err := filePath(req.Path)
	if err != nil {
		writeErr(w, err)
		return
	}
	var clip model.Clip
	switch fileKind(name) {
	case "workflow":
		clip, err = model.CopyWorkflow(name, []byte(req.Content), req.IDs)
	case "pipeline":
		clip, err = model.CopyPipeline(name, []byte(req.Content), req.IDs)
	default:
		writeError(w, http.StatusBadRequest, "only actions of workflows and transforms of pipelines can be copied")
		return
	}
	var diags model.Errors
	switch {
	case errors.As(err, &diags):
		writeJSON(w, http.StatusUnprocessableEntity, startProblems{
			Error:            "the file has errors that prevent copying from it",
			validateResponse: s.response(diags),
		})
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeJSON(w, http.StatusOK, clip)
	}
}
