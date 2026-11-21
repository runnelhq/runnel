package server

import (
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/runs"
)

// Renaming and moving files and folders (E11, E12) and the references other files make
// to them: trigger targets and calls with a fixed path (core.run-workflow,
// core.run-pipeline). On request, the references are changed to the new path.

// reference is a place in a file of the project that refers to another file.
type reference struct {
	File string `json:"file"`
	// What tells how, e.g. "target" or "action call (core.run-workflow)".
	What string `json:"what"`
	// Field is the field with the path: "target", "workflow" or "pipeline".
	Field string `json:"field"`
	// Target is the file referred to, relative to the project; for a folder one of its files.
	Target string `json:"target"`
	// Value is the path as written in the file, e.g. "./pipelines/count.pipeline.yaml".
	Value string `json:"value"`
	Line  int    `json:"line"`
}

// refKey names a reference to change when renaming: its file and line.
type refKey struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

type renameRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
	// References to change to the new path (E12); the others stay as they are.
	References []refKey `json:"references,omitempty"`
}

// notUpdated is a reference that could not be changed, with the reason.
type notUpdated struct {
	refKey
	Reason string `json:"reason"`
}

type renameResponse struct {
	Path string `json:"path"`
	// Updated are the files whose references were changed (at their new place).
	Updated    []string     `json:"updated"`
	NotUpdated []notUpdated `json:"notUpdated"`
}

// handleRename renames or moves a file or folder within the project; 409 if the new
// name exists. References given in the request are changed to the new path (E12).
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
	case from == model.ProjectFile:
		writeError(w, http.StatusConflict, projectFileNeeded)
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
	// The references are found before the move, at the old paths.
	var refs []reference
	if len(req.References) > 0 {
		refs = s.references(from)
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
	res := renameResponse{Path: to, Updated: []string{}, NotUpdated: []notUpdated{}}
	if len(req.References) > 0 {
		s.updateReferences(&res, refs, req.References, from, to)
	}
	writeJSON(w, http.StatusOK, res)
}

// moved returns where a path is after from was renamed to to.
func moved(p, from, to string) string {
	if p == from {
		return to
	}
	if rest, ok := strings.CutPrefix(p, from+"/"); ok {
		return to + "/" + rest
	}
	return p
}

// updateReferences changes the chosen references to the new paths, one line each.
// A reference that is no longer there (e.g. the file changed) is reported, not guessed.
func (s *Server) updateReferences(res *renameResponse, refs []reference, chosen []refKey, from, to string) {
	byFile := map[string][]reference{}
	var files []string
	for _, k := range chosen {
		i := -1
		for j, ref := range refs {
			if ref.File == k.File && ref.Line == k.Line {
				i = j
				break
			}
		}
		if i < 0 {
			res.NotUpdated = append(res.NotUpdated, notUpdated{k, "the reference was not found (the file may have changed)"})
			continue
		}
		if byFile[k.File] == nil {
			files = append(files, k.File)
		}
		byFile[k.File] = append(byFile[k.File], refs[i])
	}
	for _, file := range files {
		at := moved(file, from, to)
		data, err := s.root.ReadFile(filepath.FromSlash(at))
		if err == nil && !utf8.Valid(data) {
			err = fmt.Errorf("only UTF-8 files can be changed")
		}
		var lines []string
		if err == nil {
			lines = strings.Split(string(data), "\n")
			for _, ref := range byFile[file] {
				if !replaceInLine(lines, ref.Line, ref.Value, moved(ref.Target, from, to)) {
					err = fmt.Errorf("%s was not found in line %d", ref.Value, ref.Line)
					break
				}
			}
		}
		if err == nil {
			err = s.writeFile(at, []byte(strings.Join(lines, "\n")))
		}
		if err != nil {
			for _, ref := range byFile[file] {
				res.NotUpdated = append(res.NotUpdated, notUpdated{refKey{ref.File, ref.Line}, publicReason(err)})
			}
			continue
		}
		res.Updated = append(res.Updated, at)
	}
}

// publicReason is the reason for a reference not updated, without internal details.
func publicReason(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "UTF-8") || strings.Contains(msg, "was not found in line") {
		return msg
	}
	return "the file could not be changed"
}

// replaceInLine replaces the path old by repl in a line (1-based) and reports whether it
// was there.
func replaceInLine(lines []string, line int, old, repl string) bool {
	if line < 1 || line > len(lines) || old == "" {
		return false
	}
	l := lines[line-1]
	i := strings.Index(l, old)
	if i < 0 {
		return false
	}
	lines[line-1] = l[:i] + repl + l[i+len(old):]
	return true
}

// handleReferences lists the saved files that refer to a file, or to a file in a folder.
// Used before deleting, renaming and moving (E11, E12).
func (s *Server) handleReferences(w http.ResponseWriter, r *http.Request) {
	name, err := filePath(r.PathValue("path"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]reference{"references": s.references(name)})
}

// references returns the trigger targets and the calls with a fixed path
// (core.run-workflow, core.run-pipeline) that refer to name or to a file in the folder
// name. References within the folder itself count too, as their targets move.
func (s *Server) references(name string) []reference {
	refs := []reference{}
	within := func(target string) bool { return target == name || strings.HasPrefix(target, name+"/") }
	triggers, _ := model.LoadTriggers(s.dir) // files with problems are left out
	for _, t := range triggers {
		if target := path.Clean(filepath.ToSlash(t.Target)); within(target) {
			refs = append(refs, reference{File: s.relative(t.File()), What: "target", Field: "target", Target: target, Value: t.Target,
				Line: t.ErrorAt("$.target", "").Pos.Line})
		}
	}
	_ = fs.WalkDir(s.root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case d.IsDir() && p != "." && strings.HasPrefix(d.Name(), "."):
			return fs.SkipDir
		case d.IsDir() || !strings.HasSuffix(p, ".workflow.yaml"):
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
		for i, a := range wf.Actions {
			key := map[string]string{"core.run-workflow": "workflow", "core.run-pipeline": "pipeline"}[a.Type]
			if key == "" {
				continue
			}
			value, ok := runs.StaticText(a.With[key])
			if !ok || value == "" {
				continue
			}
			if _, target := runs.Resolve(s.dir, filepath.Join(s.dir, p), value); within(target) && target != p {
				at := fmt.Sprintf("$.actions[%d].with.%s", i, key)
				refs = append(refs, reference{File: p, What: fmt.Sprintf("action %s (%s)", a.ID, a.Type), Field: key, Target: target, Value: value,
					Line: wf.ErrorAt(at, "").Pos.Line})
			}
		}
		return nil
	})
	return refs
}
