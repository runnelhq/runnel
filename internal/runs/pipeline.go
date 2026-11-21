package runs

import (
	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/engine/pipeline"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// PipelineSpec describes a pipeline run to prepare, like Spec for workflows.
type PipelineSpec struct {
	// Name identifies the pipeline file in messages.
	Name string
	Data []byte
	// ProjectDir is the directory of the project; "" without a project.
	ProjectDir string
	// Env names the environment; "" selects the project's default environment.
	Env    string
	Params map[string]string

	Registry *transform.Registry
	Observer func(pipeline.Event)
	RunID    string
	// Trigger names what started the run, e.g. "cron:nightly"; empty: started by hand.
	Trigger string
	// Client is the address of the caller of an http trigger.
	Client string
	// Rows are records for core.from-result (from core.run-pipeline, or the JSON list
	// in the body of a request to an http trigger).
	Rows []record.Record
	// Respond answers the waiting caller of an http trigger (http.response, S38c).
	Respond action.Responder
	// History: see Spec.History.
	History string
	// Preview runs only a part of the pipeline as data preview (S44); nil: all of it.
	Preview *PreviewSpec
}

// PreparedPipeline is the result of PreparePipeline; it is also returned when it fails,
// holding what was found up to the failure.
type PreparedPipeline struct {
	Runner   *pipeline.Runner
	Pipeline *model.Pipeline
	Project  *model.Project
	Env      *model.Environment
	Warnings model.Errors
	// Undeclared lists parameters given at start but not declared by the pipeline.
	Undeclared []string
	// Preview relates the links of a preview to those of the pipeline (PipelineSpec.Preview).
	Preview Preview
}

// PreparePipeline loads, checks and prepares a pipeline run (roadmap S13b). Problems in
// the files are returned as model.Errors, other problems as plain errors.
func PreparePipeline(spec PipelineSpec) (*PreparedPipeline, error) {
	p := &PreparedPipeline{}
	pl, diags, err := parsePipeline(spec.Name, spec.Data)
	if err != nil {
		return p, err
	}
	p.Pipeline = pl
	if diags.HasErrors() {
		return p, diags
	}
	p.Warnings = diags

	if p.Project, p.Env, err = scope(spec.ProjectDir, spec.Env, spec.Name); err != nil {
		return p, err
	}
	if p.Undeclared, err = checkParams(pl.Parameters, spec.Params); err != nil {
		return p, err
	}
	envName := ""
	if p.Env != nil {
		envName = p.Env.Name
	}
	run, opts := pl, pipeline.Options{}
	if spec.Preview != nil {
		if run, p.Preview, err = previewPipeline(pl, spec.Preview.UpTo, spec.Registry); err != nil {
			return p, err
		}
		limit := min(max(spec.Preview.Limit, 1), MaxPreviewLimit)
		opts.InputLimit, opts.SampleRows = limit, limit
	}
	p.Runner, err = pipeline.Prepare(run, pipeline.Options{
		InputLimit:  opts.InputLimit,
		SampleRows:  opts.SampleRows,
		Registry:    spec.Registry,
		Scope:       model.PipelineScope(pl, p.Project, p.Env, spec.Params),
		Environment: envName,
		Observer:    spec.Observer,
		RunID:       spec.RunID,
		Rows:        spec.Rows,
		Respond:     spec.Respond,
	})
	if err == nil {
		// Warnings of the schemas, e.g. fields that are not known (ADR 0007).
		p.Warnings = append(p.Warnings, p.Runner.Warnings()...)
		p.Warnings.Sort()
	}
	return p, err
}
