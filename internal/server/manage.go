package server

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Folders (E11): with them and renaming (rename.go) a project can be built in the editor
// alone. Files are created and deleted with PUT and DELETE /files/{path}.

type pathRequest struct {
	Path string `json:"path"`
}

// handleCreateFolder creates a folder (and missing parents); 409 if it exists.
func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	var req pathRequest
	if !decodeSmall(w, r, &req) {
		return
	}
	name, err := filePath(req.Path)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := s.root.Stat(filepath.FromSlash(name)); err == nil {
		writeError(w, http.StatusConflict, name+" already exists")
		return
	}
	if err := s.root.MkdirAll(filepath.FromSlash(name), 0o755); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"path": name})
}

// handleDeleteFolder deletes an empty folder; 409 if it has files, also hidden ones.
func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	name, err := filePath(r.PathValue("path"))
	if err != nil {
		writeErr(w, err)
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	native := filepath.FromSlash(name)
	info, err := s.root.Stat(native)
	switch {
	case errors.Is(err, os.ErrNotExist):
		writeError(w, http.StatusNotFound, "folder not found")
		return
	case err != nil:
		writeErr(w, err)
		return
	case !info.IsDir():
		writeError(w, http.StatusBadRequest, name+" is a file; delete it with DELETE /files/"+name)
		return
	}
	entries, err := fs.ReadDir(s.root.FS(), name)
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(entries) > 0 {
		hidden := !slices.ContainsFunc(entries, func(e fs.DirEntry) bool { return !strings.HasPrefix(e.Name(), ".") })
		msg := "only empty folders can be deleted"
		if hidden {
			msg += " (it contains hidden files)"
		}
		writeError(w, http.StatusConflict, msg)
		return
	}
	if err := s.root.Remove(native); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeSmall reads a small JSON request; on an error it answers 400 and returns false.
func decodeSmall(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 64*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return false
	}
	return true
}
