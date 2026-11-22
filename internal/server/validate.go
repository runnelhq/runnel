package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/engine/pipeline"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/runs"
	"github.com/runnelhq/runnel/internal/schema"
	"github.com/runnelhq/runnel/internal/transform"
)

// Catalog lists the available building blocks: the actions of workflows and the transforms of pipelines.
type Catalog struct {
	Actions []action.Descriptor `json:"actions"`
	// Transforms of pipelines (roadmap S13b); a separate namespace (ADR 0013).
	Transforms []transform.Descriptor `json:"transforms"`
}

// NewCatalog builds the catalog of the registries; transforms may be nil.
func NewCatalog(actions *action.Registry, transforms *transform.Registry) Catalog {
	c := Catalog{Actions: actions.Descriptors(), Transforms: []transform.Descriptor{}}
	if transforms != nil {
		c.Transforms = transforms.Descriptors()
	}
	return c
}

func (s *Server) handleCatalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, NewCatalog(s.registry, s.transforms))
}

// handleSchema returns the JSON Schema of a file kind (project, environment, trigger;
// roadmap S58), for the help of the editor and for other editors.
func (*Server) handleSchema(w http.ResponseWriter, r *http.Request) {
	format, ok := model.FileFormat(r.PathValue("kind"))
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no schema for %q (kinds: %s)", r.PathValue("kind"), strings.Join(model.FileFormats(), ", ")))
		return
	}
	sc, err := schema.ForFile(format)
	if err != nil { // a bug in the tags of package model; the tests prevent it
		writeError(w, http.StatusInternalServerError, "cannot build the schema")
		return
	}
	writeJSON(w, http.StatusOK, sc)
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
	// Pipeline is the parsed pipeline (transforms, links, layout …), for pipeline files (S25a).
	Pipeline *model.Pipeline `json:"pipeline,omitempty"`
	// Schemas are the fields of the records around each transform of a pipeline, by
	// transform id (S25a2); missing if the pipeline has errors in its fields or graph.
	Schemas map[string]pipeline.TransformSchemas `json:"schemas,omitempty"`
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
	kind := fileKind(name)
	if kind == "other" {
		writeError(w, http.StatusBadRequest, "only workflows, pipelines, project.yaml, environments and triggers can be validated")
		return
	}
	var data []byte
	if req.Content != nil {
		data = []byte(*req.Content)
	} else if data, err = s.readFile(name); err != nil {
		writeErr(w, err)
		return
	}
	if kind == "project" || kind == "environment" || kind == "trigger" {
		diags, err := s.validateText(kind, name, data)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, s.response(diags))
		return
	}
	if kind == "pipeline" {
		p, schemas, diags, err := s.validatePipeline(name, data)
		if err != nil {
			writeErr(w, err)
			return
		}
		res := s.response(diags)
		res.Pipeline, res.Schemas = p, schemas
		writeJSON(w, http.StatusOK, res)
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

// validatePipeline checks a pipeline like "runnel validate": fields, graph and – as
// before a run – the types, configurations and fields of its transforms (ADR 0007).
// It returns the parsed pipeline (nil if it cannot be decoded), the schemas of its
// transforms (nil if its fields or graph have errors) and all problems.
func (s *Server) validatePipeline(name string, data []byte) (*model.Pipeline, map[string]pipeline.TransformSchemas, model.Errors, error) {
	p, err := model.ReadPipeline(name, data)
	var diags model.Errors
	if err != nil && !errors.As(err, &diags) {
		return nil, nil, nil, err
	}
	if p == nil {
		return nil, nil, diags, nil
	}
	diags = append(diags, p.Validate()...)
	var schemas map[string]pipeline.TransformSchemas
	if !diags.HasErrors() && s.transforms != nil {
		schemas = pipeline.Schemas(p, pipeline.Options{Registry: s.transforms})
	}
	return p, schemas, runs.CheckPipeline(p, diags, s.transforms), nil
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
	diags, err = runs.CheckWorkflow(wf, diags, runs.WorkflowCheck{Variables: p != nil, Project: p, Registry: s.registry,
		ProjectDir: s.dir, File: filepath.Join(s.dir, filepath.FromSlash(name))})
	if err != nil {
		return nil, nil, err
	}
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
