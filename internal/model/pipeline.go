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
// roadmap; S12 defines transforms and data links, S17a error links, S17c copies,
// distribution and settings, S25a the layout of the editor.
type Pipeline struct {
	Kind        string           `yaml:"kind" json:"kind"`
	APIVersion  string           `yaml:"apiVersion" json:"apiVersion"`
	Name        string           `yaml:"name" json:"name"`
	Description string           `yaml:"description,omitempty" json:"description,omitempty"`
	Parameters  []Parameter      `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	Transforms  []Transform      `yaml:"transforms,omitempty" json:"transforms,omitempty"`
	Links       []PipelineLink   `yaml:"links,omitempty" json:"links,omitempty"`
	Settings    PipelineSettings `yaml:"settings,omitempty" json:"settings,omitzero"`
	// Layout holds editor coordinates per transform id (S25a); it has no effect on execution.
	Layout map[string]Point `yaml:"layout,omitempty" json:"layout,omitempty"`

	src *source
}

// PipelineSettings control the execution of a pipeline as a whole (KONZEPT, chapter 6.2).
type PipelineSettings struct {
	// BufferSize is the number of records a link holds before the transform before it
	// waits (backpressure); 0: 10000.
	BufferSize int `yaml:"bufferSize,omitempty" json:"bufferSize,omitempty"`
	// BatchSize is the number of records passed over a link at once; 0: 500.
	BatchSize int `yaml:"batchSize,omitempty" json:"batchSize,omitempty"`
	// Strict requires a closed schema on every link; warnings about unknown fields
	// become errors (ADR 0007).
	Strict bool `yaml:"strict,omitempty" json:"strict,omitempty"`
	// OnError is the workflow started when a run fails (ADR 0027); see WorkflowSettings.
	OnError string `yaml:"onError,omitempty" json:"onError,omitempty" doc:"Workflow started when a run of this pipeline fails, relative to the project; none: no error workflow; empty: the one of project.yaml"`
}

// Limits of the settings, so that a typo does not take all memory.
const (
	MaxBatchSize  = 100_000
	MaxBufferSize = 10_000_000
	MaxCopies     = 64
)

// Distribution modes of a transform with several outgoing data links.
const (
	// DistributeCopy passes every record to every data link (the default).
	DistributeCopy = "copy"
	// DistributeRoundRobin passes each record to one data link, in turn (until R5b:
	// distribute).
	DistributeRoundRobin = "roundRobin"
)

// Transform is a step of a pipeline.
type Transform struct {
	ID   string `yaml:"id" json:"id"`
	Type string `yaml:"type" json:"type"`
	// With is the type-specific configuration of the transform.
	With map[string]any `yaml:"with,omitempty" json:"with,omitempty"`
	// Copies is the number of instances that run in parallel, sharing the incoming
	// records; 0: 1.
	Copies int `yaml:"copies,omitempty" json:"copies,omitempty"`
	// Distribution is copy (default) or roundRobin: how records are passed to several
	// outgoing data links.
	Distribution string `yaml:"distribution,omitempty" json:"distribution,omitempty"`
}

// LinkType is the kind of a pipeline link.
type LinkType string

// Link types of pipelines (KONZEPT, chapter 2.3).
const (
	// DataLink passes the records of a transform on (the default).
	DataLink LinkType = "data"
	// ErrorLink receives the records a transform could not process (roadmap S17).
	ErrorLink LinkType = "error"
	// InfoLink is a side input, e.g. the data of a lookup (roadmap S23b): the transform
	// reads it completely before its data links.
	InfoLink LinkType = "info"
)

// PipelineLink connects two transforms; records flow from "from" to "to".
type PipelineLink struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"`
	// Type is data (default), error or info (lookup data for transforms that take it,
	// S23b).
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

// ReadPipeline is like ParsePipeline, but also returns the pipeline when it has field
// errors (e.g. an invalid link type), so that the editor can show and fix it; it is nil
// only if the file cannot be decoded at all.
func ReadPipeline(name string, data []byte) (*Pipeline, error) {
	src, err := parseSource(name, data)
	if err != nil {
		return nil, err
	}
	p := &Pipeline{src: src}
	if err := src.decode(p); err != nil {
		return nil, err
	}
	return p, p.validate()
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
		if t.Copies < 0 || t.Copies > MaxCopies {
			add(path+".copies", "%s: copies must be between 1 and %d", label, MaxCopies)
		}
		switch t.Distribution {
		case "", DistributeCopy, DistributeRoundRobin:
		case "distribute":
			add(path+".distribution", "%s: distribution distribute was renamed before 1.0: use roundRobin", label)
		default:
			add(path+".distribution", "%s: invalid distribution %q (expected copy or roundRobin)", label, t.Distribution)
		}
	}
	st := p.Settings
	if st.BatchSize < 0 || st.BatchSize > MaxBatchSize {
		add("$.settings.batchSize", "settings.batchSize must be between 1 and %d", MaxBatchSize)
	}
	if st.BufferSize < 0 || st.BufferSize > MaxBufferSize {
		add("$.settings.bufferSize", "settings.bufferSize must be between 1 and %d", MaxBufferSize)
	}
	if msg := CheckOnErrorPath(st.OnError); msg != "" {
		add("$.settings.onError", "%s", msg)
	}
	if st.BatchSize > 0 && st.BufferSize > 0 && st.BufferSize < st.BatchSize {
		add("$.settings.bufferSize", "settings.bufferSize (%d) must be at least settings.batchSize (%d)", st.BufferSize, st.BatchSize)
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
		case "", DataLink, ErrorLink, InfoLink:
		default:
			add(path+".type", "%s: invalid link type %q (expected data, error or info)", label, l.Type)
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
