package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/runs"
)

// Folders, renaming and the references to a file (E11): with them a project can be built
// in the editor alone. Files are created and deleted with PUT and DELETE /files/{path}.

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

type renameRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// handleRename renames or moves a file or folder within the project; 409 if the new
// name exists. References in other files are not changed (decision for mockup E11).
func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	var req renameRequest
	if !decodeSmall(w, r, &req) {
		return
	}
	from, err := filePath(req.From)
	if err == nil {
		req.To, err = filePath(req.To)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	to := req.To
	switch {
	case from == to:
		writeError(w, http.StatusBadRequest, "the new name is the old one")
		return
	case strings.HasPrefix(to, from+"/"):
		writeError(w, http.StatusBadRequest, "a folder cannot be moved into itself")
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := s.root.Stat(filepath.FromSlash(from)); err != nil {
		writeErr(w, err)
		return
	}
	// Only a different case of the same name may exist (Windows ignores case).
	if _, err := s.root.Stat(filepath.FromSlash(to)); err == nil && !strings.EqualFold(from, to) {
		writeError(w, http.StatusConflict, to+" already exists")
		return
	}
	if dir := path.Dir(to); dir != "." {
		if err := s.root.MkdirAll(filepath.FromSlash(dir), 0o755); err != nil {
			writeErr(w, err)
			return
		}
	}
	if err := s.root.Rename(filepath.FromSlash(from), filepath.FromSlash(to)); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": to})
}

// reference is a file of the project that refers to another one.
type reference struct {
	File string `json:"file"`
	// What tells how, e.g. "target" or "action call (core.run-workflow)".
	What string `json:"what"`
}

// handleReferences lists the saved files that refer to a workflow or pipeline: triggers
// with it as target and workflows that call it with a fixed path (core.run-workflow,
// core.run-pipeline). Used before deleting a file (E11).
func (s *Server) handleReferences(w http.ResponseWriter, r *http.Request) {
	name, err := filePath(r.PathValue("path"))
	if err != nil {
		writeErr(w, err)
		return
	}
	refs := []reference{}
	triggers, _ := model.LoadTriggers(s.dir) // files with problems are left out
	for _, t := range triggers {
		if path.Clean(filepath.ToSlash(t.Target)) == name {
			refs = append(refs, reference{File: s.relative(t.File()), What: "target"})
		}
	}
	_ = fs.WalkDir(s.root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case d.IsDir() && p != "." && strings.HasPrefix(d.Name(), "."):
			return fs.SkipDir
		case d.IsDir() || !strings.HasSuffix(p, ".workflow.yaml") || p == name:
			return nil
		}
		data, err := s.root.ReadFile(filepath.FromSlash(p))
		if err != nil {
			return nil
		}
		wf, err := model.ParseWorkflow(p, data)
		if err != nil {
			return nil
		}
		for _, a := range wf.Actions {
			key := map[string]string{"core.run-workflow": "workflow", "core.run-pipeline": "pipeline"}[a.Type]
			if key == "" {
				continue
			}
			if target, ok := runs.StaticText(a.With[key]); ok && target != "" {
				if _, resolved := runs.Resolve(s.dir, filepath.Join(s.dir, p), target); resolved == name {
					refs = append(refs, reference{File: p, What: fmt.Sprintf("action %s (%s)", a.ID, a.Type)})
				}
			}
		}
		return nil
	})
	writeJSON(w, http.StatusOK, map[string][]reference{"references": refs})
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
