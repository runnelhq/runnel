package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/runnelhq/runnel/internal/model"
)

// editRequest applies changes made on the canvas to the text the editor shows.
type editRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	model.WorkflowEdit
}

type editResponse struct {
	Content string `json:"content"`
}

// handleEditWorkflow changes the text of a workflow (move, delete) and returns the new
// text; it does not save. Comments and formatting of untouched lines stay as they are.
func (s *Server) handleEditWorkflow(w http.ResponseWriter, r *http.Request) {
	var req editRequest
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
	if fileKind(name) != "workflow" {
		writeError(w, http.StatusBadRequest, "only workflow files (*.workflow.yaml) can be edited on the canvas")
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
