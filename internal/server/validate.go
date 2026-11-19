package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/model"
)

// Catalog lists the available building blocks; transforms follow with the pipelines.
type Catalog struct {
	Actions []action.Descriptor `json:"actions"`
}

// NewCatalog builds the catalog of a registry.
func NewCatalog(r *action.Registry) Catalog {
	return Catalog{Actions: r.Descriptors()}
}

func (s *Server) handleCatalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, NewCatalog(s.registry))
}

// validateRequest names the workflow to check. Content, if given, is checked instead
// of the saved file – the editor sends its unsaved state.
type validateRequest struct {
	Path    string  `json:"path"`
	Content *string `json:"content,omitempty"`
}

// Problem is an error or warning found by /validate.
type Problem struct {
	// File is relative to the project directory, with "/" as separator.
	File   string `json:"file"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
	// Path is the YAML path of the element, e.g. "$.actions[2].with", if known.
	Path     string `json:"path,omitempty"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type validateResponse struct {
	Errors   int       `json:"errors"`
	Warnings int       `json:"warnings"`
	Problems []Problem `json:"problems"`
	// Workflow is the parsed workflow (actions, links, layout …) for the canvas; it is
	// missing if the file cannot be read as a workflow (syntax or field errors).
	Workflow *model.Workflow `json:"workflow,omitempty"`
}

// handleValidate checks a workflow like "runnel validate" does: fields, links, graph,
// variables against every environment of the project, and – as before a run – action
// types and their configuration.
func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	var req validateRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxFileSize+1024))
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
		writeError(w, http.StatusBadRequest, "only workflow files (*.workflow.yaml) can be validated")
		return
	}
	var data []byte
	if req.Content != nil {
		data = []byte(*req.Content)
	} else if data, err = s.readFile(name); err != nil {
		writeErr(w, err)
		return
	}
	wf, diags, err := s.validate(name, data)
	if err != nil {
		writeErr(w, err)
		return
	}
	res := s.response(diags)
	res.Workflow = wf
	writeJSON(w, http.StatusOK, res)
}

// validate returns the parsed workflow (nil if it cannot be parsed) and all problems.
func (s *Server) validate(name string, data []byte) (*model.Workflow, model.Errors, error) {
	// Workflows with field errors are returned as well, so that the editor can show them.
	wf, err := model.ReadWorkflow(name, data)
	var diags model.Errors
	if err != nil && !errors.As(err, &diags) {
		return nil, nil, err
	}
	if wf == nil {
		return nil, diags, nil
	}
	diags = append(diags, wf.Validate()...)

	p, err := model.LoadProject(s.dir)
	var projectDiags model.Errors
	if errors.As(err, &projectDiags) {
		diags = append(diags, projectDiags...)
		if projectDiags.HasErrors() {
			p = nil
		}
	} else if err != nil {
		return nil, nil, err
	}
	if p != nil {
		contexts, err := model.VarContexts(wf, p, "", nil)
		if err != nil {
			return nil, nil, err
		}
		diags = append(diags, wf.CheckVariables(contexts)...)
	}
	if !diags.HasErrors() {
		diags = append(diags, workflow.Check(wf, workflow.Options{Registry: s.registry})...)
	}
	diags.Sort()
	return wf, diags, nil
}

func (s *Server) response(diags model.Errors) validateResponse {
	res := validateResponse{Problems: []Problem{}}
	for _, d := range diags {
		res.Problems = append(res.Problems, Problem{
			File:     s.relative(d.File),
			Line:     d.Pos.Line,
			Column:   d.Pos.Column,
			Path:     d.Path,
			Severity: d.Severity.String(),
			Message:  d.Msg,
		})
		if d.Severity == model.SeverityWarning {
			res.Warnings++
		} else {
			res.Errors++
		}
	}
	return res
}

// relative turns file names of project files (absolute) into paths relative to the project.
func (s *Server) relative(file string) string {
	if !filepath.IsAbs(file) {
		return filepath.ToSlash(file)
	}
	rel, err := filepath.Rel(s.dir, file)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(file)
	}
	return filepath.ToSlash(rel)
}
