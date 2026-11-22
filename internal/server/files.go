package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/model"
)

// MaxFileSize is the largest file the API accepts for writing.
const MaxFileSize = 10 << 20

// Entry is a file or directory of the project tree.
type Entry struct {
	Name string `json:"name"`
	// Path is relative to the project directory, with "/" as separator.
	Path string `json:"path"`
	// Type is "dir" or "file".
	Type string `json:"type"`
	// Kind tells what a file contains: workflow, pipeline, project, environment or other.
	Kind     string   `json:"kind,omitempty"`
	Size     int64    `json:"size,omitempty"`
	Children []*Entry `json:"children,omitempty"`
}

type projectInfo struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
	// Environments are the names of the environments, sorted (for the run dialog, E8a).
	Environments []string `json:"environments"`
	// DefaultEnvironment is used when a run names no environment; empty if none is set.
	DefaultEnvironment string `json:"defaultEnvironment,omitempty"`
}

func (s *Server) handleProject(w http.ResponseWriter, _ *http.Request) {
	info := projectInfo{Dir: s.dir, Environments: []string{}}
	if p, _ := model.LoadProject(s.dir); p != nil {
		info.Name = p.Name
		info.Environments = p.EnvironmentNames()
		info.DefaultEnvironment = p.DefaultEnvironment
	}
	writeJSON(w, http.StatusOK, info)
}

// handleTree lists the project directory recursively. Hidden entries (".git" …) and
// symbolic links are left out.
func (s *Server) handleTree(w http.ResponseWriter, _ *http.Request) {
	tree := &Entry{Name: filepath.Base(s.dir), Path: "", Type: "dir"}
	dirs := map[string]*Entry{".": tree}
	err := fs.WalkDir(s.root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || d.Type()&fs.ModeSymlink != 0 {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		e := &Entry{Name: d.Name(), Path: p, Type: "file"}
		if d.IsDir() {
			e.Type = "dir"
			dirs[p] = e
		} else {
			e.Kind = fileKind(p)
			if info, err := d.Info(); err == nil {
				e.Size = info.Size()
			}
		}
		parent := dirs[path.Dir(p)]
		parent.Children = append(parent.Children, e)
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	sortTree(tree)
	writeJSON(w, http.StatusOK, tree)
}

// sortTree orders directories before files, each by name.
func sortTree(e *Entry) {
	slices.SortFunc(e.Children, func(a, b *Entry) int {
		if a.Type != b.Type {
			if a.Type == "dir" {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	for _, c := range e.Children {
		sortTree(c)
	}
}

// fileKind classifies a project file by its path.
func fileKind(p string) string {
	switch {
	case strings.HasSuffix(p, ".workflow.yaml"):
		return "workflow"
	case strings.HasSuffix(p, ".pipeline.yaml"):
		return "pipeline"
	case p == model.ProjectFile:
		return "project"
	case path.Dir(p) == model.EnvironmentsDir && strings.HasSuffix(p, ".yaml"):
		return "environment"
	case model.IsTriggerFile(p):
		return "trigger"
	case strings.EqualFold(path.Ext(p), ".md"):
		return "document"
	case imageTypes[strings.ToLower(path.Ext(p))] != "":
		return "image"
	}
	return "other"
}

func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	name, err := filePath(r.PathValue("path"))
	if err != nil {
		writeErr(w, err)
		return
	}
	data, err := s.readFile(name)
	if err != nil {
		writeErr(w, err)
		return
	}
	h := w.Header()
	h.Set("ETag", etag(data))
	h.Set("Content-Type", contentType(name))
	h.Set("Cache-Control", "no-store")
	// A file of the project is data, never a page of the editor: no guessing of its type,
	// and opened directly in the browser it runs no scripts (e.g. an SVG or HTML file).
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	_, _ = w.Write(data)
}

// handlePutFile creates or replaces a file. Replacing requires If-Match with the
// current ETag, so that changes made in the meantime (another browser, an editor,
// git) are not overwritten; creating fails if the file already exists.
func (s *Server) handlePutFile(w http.ResponseWriter, r *http.Request) {
	name, err := filePath(r.PathValue("path"))
	if err != nil {
		writeErr(w, err)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxFileSize))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "file is larger than 10 MiB")
			return
		}
		writeErr(w, err)
		return
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	created, err := s.checkPrecondition(name, r.Header.Get("If-Match"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.writeFile(name, data); err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("ETag", etag(data))
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]string{"path": name, "etag": etag(data)})
}

// projectFileNeeded refuses to delete, rename or move project.yaml: without it,
// "runnel serve" does not start again (or finds another project above the folder).
const projectFileNeeded = model.ProjectFile + " cannot be deleted, renamed or moved: the project needs it"

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	name, err := filePath(r.PathValue("path"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if name == model.ProjectFile {
		writeError(w, http.StatusConflict, projectFileNeeded)
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if r.Header.Get("If-Match") == "" {
		writeError(w, http.StatusPreconditionRequired, "If-Match header with the file's ETag is required")
		return
	}
	created, err := s.checkPrecondition(name, r.Header.Get("If-Match"))
	if err == nil && created {
		err = os.ErrNotExist
	}
	if err == nil {
		err = s.root.Remove(filepath.FromSlash(name))
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// checkPrecondition compares If-Match with the current file. It reports whether
// the file does not exist yet (and will be created).
func (s *Server) checkPrecondition(name, ifMatch string) (create bool, err error) {
	current, err := s.readFile(name)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if ifMatch != "" && ifMatch != "*" {
			return false, errorf(http.StatusPreconditionFailed, "file was deleted in the meantime")
		}
		return true, nil
	case err != nil:
		return false, err
	case ifMatch == "":
		return false, errorf(http.StatusPreconditionRequired, "file exists; If-Match header with its ETag is required to replace it")
	case ifMatch != "*" && ifMatch != etag(current):
		return false, errorf(http.StatusPreconditionFailed, "file was changed in the meantime; reload it before saving")
	}
	return false, nil
}

func (s *Server) readFile(name string) ([]byte, error) {
	info, err := s.root.Stat(filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, errorf(http.StatusBadRequest, "%s is a directory", name)
	}
	return s.root.ReadFile(filepath.FromSlash(name))
}

// writeFile writes a file atomically: first to a hidden temporary file in the same
// directory, then renamed, so that a crash never leaves a half-written file.
func (s *Server) writeFile(name string, data []byte) error {
	native := filepath.FromSlash(name)
	dir := filepath.Dir(native)
	if err := s.root.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, ".runnel-"+randomSuffix()+".tmp")
	if err := s.root.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := s.root.Rename(tmp, native); err != nil {
		_ = s.root.Remove(tmp)
		return err
	}
	return nil
}

func randomSuffix() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// etag identifies the content of a file.
func etag(data []byte) string {
	sum := sha256.Sum256(data)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

// imageTypes are the images a Markdown document may show (E17); SVG is left out because
// it can contain scripts.
var imageTypes = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}

func contentType(name string) string {
	if t := imageTypes[strings.ToLower(path.Ext(name))]; t != "" {
		return t
	}
	switch path.Ext(name) {
	case ".yaml", ".yml":
		return "application/yaml; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".txt", ".md", ".csv":
		return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

// filePath checks a path from the URL. It must be relative to the project directory,
// use "/" as separator and name no hidden entries ("." prefix) or names that Windows
// cannot store or treats specially (device names, ":", trailing dots or spaces …).
// os.Root additionally prevents escaping the project through symbolic links.
func filePath(p string) (string, error) {
	if p == "" {
		return "", errorf(http.StatusBadRequest, "missing file path")
	}
	if strings.Contains(p, `\`) {
		return "", errorf(http.StatusBadRequest, `invalid path %q: use "/" as separator`, p)
	}
	for _, seg := range strings.Split(p, "/") {
		if err := checkSegment(seg); err != "" {
			return "", errorf(http.StatusBadRequest, "invalid path %q: %s", p, err)
		}
	}
	return p, nil
}

func checkSegment(seg string) string {
	switch {
	case seg == "" || seg == "." || seg == "..":
		return "empty, \".\" or \"..\" segments are not allowed"
	case strings.HasPrefix(seg, "."):
		return "hidden files and directories cannot be accessed"
	case strings.ContainsAny(seg, `<>:"|?*`) || strings.ContainsFunc(seg, func(r rune) bool { return r < 32 }):
		return `names must not contain < > : " | ? * or control characters`
	case strings.HasSuffix(seg, " ") || strings.HasSuffix(seg, "."):
		return "names must not end with a space or a dot"
	case isReservedName(seg):
		return "reserved Windows device name"
	}
	return ""
}

// isReservedName reports Windows device names such as CON, NUL or COM1 (also with extension).
func isReservedName(seg string) bool {
	base, _, _ := strings.Cut(strings.ToUpper(seg), ".")
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) {
		return base[3] >= '0' && base[3] <= '9'
	}
	return false
}
