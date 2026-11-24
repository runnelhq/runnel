package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/connection"
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
	writeJSON(w, http.StatusOK, NewCatalog(runs.WithPlugins(s.registry, s.dir), s.transforms))
}

// handleSchema returns the JSON Schema of a file kind (project, environment, trigger;
// roadmap S58), for the help of the editor and for other editors.
func (s *Server) handleSchema(w http.ResponseWriter, r *http.Request) {
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
	switch r.PathValue("kind") {
	case "connection":
		connectionVariants(sc, runs.ConnectionTypes(s.dir))
	case "trigger":
		triggerVariants(sc)
	}
	writeJSON(w, http.StatusOK, sc)
}

// connectionVariants adds the connection types to the schema of connection files: the
// allowed values of type and the fields under with: for each type, the built-in ones and
// those of the trusted plugins of the project (S49c).
func connectionVariants(sc *schema.Schema, reg *connection.Registry) {
	for _, p := range sc.Properties {
		switch p.Name {
		case "type":
			for _, t := range reg.Types() {
				p.Schema.Enum = append(p.Schema.Enum, t)
			}
		case "with":
			p.Schema.Variants = map[string]*schema.Schema{}
			for _, t := range reg.Types() {
				d, _ := reg.Descriptor(t)
				v := *d.Schema
				v.Schema = ""
				v.Description = d.Label + ": " + d.Description
				p.Schema.Variants[t] = &v
			}
		}
	}
}

// triggerRequired are the fields under with: each trigger type needs (as checked by
// model.ParseTrigger).
var triggerRequired = map[string][]string{
	model.TriggerCron: {"schedule"}, model.TriggerInterval: {"every"},
	model.TriggerFile: {"path"}, model.TriggerHTTP: {"method", "path", "auth"},
}

// triggerVariants adds the fields under with: for each trigger type to the schema of
// trigger files (E20a), like connectionVariants: the form and the help show only those
// of the chosen type.
func triggerVariants(sc *schema.Schema) {
	for _, p := range sc.Properties {
		if p.Name != "with" {
			continue
		}
		p.Schema.Variants = map[string]*schema.Schema{}
		for _, typ := range []string{model.TriggerCron, model.TriggerInterval, model.TriggerFile, model.TriggerHTTP} {
			v := &schema.Schema{Type: "object", AdditionalProperties: false}
			for _, f := range model.TriggerFields(typ) {
				if fs, ok := p.Schema.Properties.Lookup(f); ok {
					c := *fs
					c.Description = typeText(fs.Description, typ)
					v.Properties = append(v.Properties, schema.Property{Name: f, Schema: &c})
				}
			}
			v.Required = triggerRequired[typ]
			p.Schema.Variants[typ] = v
		}
	}
}

// typePrefix is the start of a part of a description that names the trigger types it is
// for, e.g. "cron, interval, file: ".
var typePrefix = regexp.MustCompile(`^((cron|interval|file|http)(, )?)+: `)

// typeText keeps the parts of the description of a field under with: that are for the
// given trigger type, without naming the types: "file: folder …; http: path …" gives
// "path …" for http.
func typeText(desc, typ string) string {
	var out []string
	for part := range strings.SplitSeq(desc, "; ") {
		if m := typePrefix.FindString(part); m != "" {
			if !slices.Contains(strings.Split(strings.TrimSuffix(m, ": "), ", "), typ) {
				continue
			}
			part = part[len(m):]
		}
		out = append(out, part)
	}
	if len(out) == 0 {
		return desc
	}
	text := strings.Join(out, "; ")
	return strings.ToUpper(text[:1]) + text[1:]
}

// connectionInfo names a connection of the project for the editor (GET /connections).
type connectionInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
	File string `json:"file"`
}

// handleConnections lists the connections of the project with their types, e.g. for the
// choice of connection: in the panel; files with errors are left out.
func (s *Server) handleConnections(w http.ResponseWriter, _ *http.Request) {
	list := []connectionInfo{}
	if p, _ := model.LoadProject(s.dir); p != nil {
		for _, name := range slices.Sorted(maps.Keys(p.Connections)) {
			list = append(list, connectionInfo{Name: name, Type: p.Connections[name].Type, File: path.Join(model.ConnectionsDir, name+".yaml")})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": list})
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
		writeError(w, http.StatusBadRequest, "only workflows, pipelines, project.yaml, environments, triggers and connections can be validated")
		return
	}
	var data []byte
	if req.Content != nil {
		data = []byte(*req.Content)
	} else if data, err = s.readFile(name); err != nil {
		writeErr(w, err)
		return
	}
	if kind == "project" || kind == "environment" || kind == "trigger" || kind == "connection" {
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
	project, _ := model.LoadProject(s.dir) // for the connections; a project with errors still counts
	return p, schemas, runs.CheckPipeline(p, diags, s.transforms, project), nil
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
	if projectDiags, ok := errors.AsType[model.Errors](err); ok {
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
