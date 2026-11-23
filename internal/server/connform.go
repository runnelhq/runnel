package server

import (
	"encoding/json"
	"net/http"

	"github.com/runnelhq/runnel/internal/model"
)

// connectionEditRequest carries the text the editor shows; Set changes one field (form
// of S39c2a), without Set the text is only read.
type connectionEditRequest struct {
	Path    string                 `json:"path"`
	Content string                 `json:"content"`
	Set     *model.ConnectionField `json:"set,omitempty"`
}

// connectionForm is what the form shows: the fields of the connection as written.
type connectionForm struct {
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Description string         `json:"description,omitempty"`
	With        map[string]any `json:"with"`
}

type connectionEditResponse struct {
	Content    string          `json:"content"`
	Connection *connectionForm `json:"connection,omitempty"`
}

// handleEditConnection reads the text of a connection file for the form and applies a
// change of one field (S39c2a), like /workflows/edit for the canvas; nothing is saved.
func (s *Server) handleEditConnection(w http.ResponseWriter, r *http.Request) {
	var req connectionEditRequest
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
	if fileKind(name) != "connection" {
		writeError(w, http.StatusBadRequest, "only connection files (connections/*.yaml) can be edited in the form")
		return
	}
	out := []byte(req.Content)
	if req.Set != nil {
		if out, err = model.EditConnection(name, out, *req.Set); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	res := connectionEditResponse{Content: string(out)}
	// The form shows a file that reads; without connection it asks for the YAML view.
	c, _ := model.ReadConnection(name, out)
	if c != nil {
		with := c.With
		if with == nil {
			with = map[string]any{}
		}
		res.Connection = &connectionForm{Name: c.Name, Type: c.Type, Description: c.Description, With: with}
	}
	writeJSON(w, http.StatusOK, res)
}
