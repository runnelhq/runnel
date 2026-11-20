package model

import (
	"fmt"
	"os"
	"path/filepath"
)

// KindPipeline is the kind of *.pipeline.yaml files.
const KindPipeline = "Pipeline"

// PipelineRoots are the names an expression in a pipeline parameter may use; the
// record of a transform ("record") follows with the pipeline engine (roadmap S13/S14).
var PipelineRoots = []string{"vars", "params", "env", "run"}

// Pipeline is the content of a *.pipeline.yaml file: transforms connected by links,
// through which records flow (KONZEPT, chapter 2.3). The format grows with the
// roadmap; S12 defines transforms and data links, S17a error links; further fields
// follow with the steps that execute them (copies and settings with S17c).
type Pipeline struct {
	Kind        string         `yaml:"kind" json:"kind"`
	APIVersion  string         `yaml:"apiVersion" json:"apiVersion"`
	Name        string         `yaml:"name" json:"name"`
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Parameters  []Parameter    `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	Transforms  []Transform    `yaml:"transforms,omitempty" json:"transforms,omitempty"`
	Links       []PipelineLink `yaml:"links,omitempty" json:"links,omitempty"`

	src *source
}

// Transform is a step of a pipeline.
type Transform struct {
	ID   string `yaml:"id" json:"id"`
	Type string `yaml:"type" json:"type"`
	// With is the type-specific configuration of the transform.
	With map[string]any `yaml:"with,omitempty" json:"with,omitempty"`
}

// LinkType is the kind of a pipeline link.
type LinkType string

// Link types of pipelines (KONZEPT, chapter 2.3).
const (
	// DataLink passes the records of a transform on (the default).
	DataLink LinkType = "data"
	// ErrorLink receives the records a transform could not process (roadmap S17).
	ErrorLink LinkType = "error"
	// InfoLink is a side input, e.g. the data of a lookup (roadmap S23).
	InfoLink LinkType = "info"
)

// PipelineLink connects two transforms; records flow from "from" to "to".
type PipelineLink struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"`
	// Type is data (default) or error; info is reserved for a later step.
	Type LinkType `yaml:"type,omitempty" json:"type,omitempty"`
}

// LoadPipeline reads and parses a pipeline file.
func LoadPipeline(path string) (*Pipeline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParsePipeline(filepath.ToSlash(path), data)
}

// ParsePipeline parses a pipeline from YAML. name is used in error messages.
// The returned error is of type Errors and lists all problems found.
func ParsePipeline(name string, data []byte) (*Pipeline, error) {
	src, err := parseSource(name, data)
	if err != nil {
		return nil, err
	}
	p := &Pipeline{src: src}
	if err := src.decode(p); err != nil {
		return nil, err
	}
	if err := p.validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// validate checks each field on its own; references between elements are checked by Validate.
func (p *Pipeline) validate() error {
	s := p.src
	var errs Errors
	add := func(path, format string, args ...any) {
		errs = append(errs, s.errorf(path, format, args...))
	}
	errs = append(errs, checkHeader(s, p.Kind, KindPipeline, p.APIVersion, p.Name)...)
	errs = append(errs, checkParameters(s, p.Parameters, PipelineRoots)...)

	for i, t := range p.Transforms {
		path := fmt.Sprintf("$.transforms[%d]", i)
		label := fmt.Sprintf("transform #%d", i+1)
		switch {
		case t.ID == "":
			add(path, `%s: missing required field "id"`, label)
		case !idPattern.MatchString(t.ID):
			add(path+".id", "invalid transform id %q (letters, digits, - and _, starting with a letter)", t.ID)
		default:
			label = fmt.Sprintf("transform %q", t.ID)
		}
		switch {
		case t.Type == "":
			add(path, `%s: missing required field "type"`, label)
		case !actionTypePattern.MatchString(t.Type):
			add(path+".type", `%s: invalid type %q (expected "<namespace>.<name>", e.g. "csv.input")`, label, t.Type)
		}
	}

	for i, l := range p.Links {
		path := fmt.Sprintf("$.links[%d]", i)
		label := fmt.Sprintf("link #%d", i+1)
		if l.From == "" {
			add(path, `%s: missing required field "from"`, label)
		}
		if l.To == "" {
			add(path, `%s: missing required field "to"`, label)
		}
		switch l.Type {
		case "", DataLink, ErrorLink:
		case InfoLink:
			add(path+".type", "%s: info links are not supported yet (roadmap S23)", label)
		default:
			add(path+".type", "%s: invalid link type %q (expected data or error)", label, l.Type)
		}
	}
	return errs.errOrNil()
}

// WarningAt creates a warning located at a YAML path of the pipeline file.
func (p *Pipeline) WarningAt(path, format string, args ...any) *Error {
	if p.src == nil {
		return &Error{Severity: SeverityWarning, Msg: fmt.Sprintf(format, args...)}
	}
	return p.src.warnf(path, format, args...)
}

// ErrorAt creates an error located at a YAML path of the pipeline file, e.g. "$.transforms[2].type".
func (p *Pipeline) ErrorAt(path, format string, args ...any) *Error {
	if p.src == nil {
		return &Error{Msg: fmt.Sprintf(format, args...)}
	}
	return p.src.errorf(path, format, args...)
}
