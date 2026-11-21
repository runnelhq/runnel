package runs

import (
	"fmt"

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
	// Rows are records for core.from-result (from core.run-pipeline).
	Rows []record.Record
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
}

// PreparePipeline loads, checks and prepares a pipeline run (roadmap S13b). Problems in
// the files are returned as model.Errors, other problems as plain errors.
func PreparePipeline(spec PipelineSpec) (*PreparedPipeline, error) {
	p := &PreparedPipeline{}
	pl, err := model.ParsePipeline(spec.Name, spec.Data)
	if err != nil {
		return p, err
	}
	p.Pipeline = pl
	diags := pl.Validate()
	if diags.HasErrors() {
		return p, diags
	}
	p.Warnings = diags

	if spec.ProjectDir != "" {
		if p.Project, err = model.LoadProject(spec.ProjectDir); err != nil {
			return p, err
		}
		if p.Env, err = p.Project.SelectEnvironment(spec.Env); err != nil {
			return p, err
		}
	} else if spec.Env != "" {
		return p, fmt.Errorf("environment %s: no project (%s) found for %s", spec.Env, model.ProjectFile, spec.Name)
	}
	if p.Undeclared, err = checkParams(pl.Parameters, spec.Params); err != nil {
		return p, err
	}
	envName := ""
	if p.Env != nil {
		envName = p.Env.Name
	}
	p.Runner, err = pipeline.Prepare(pl, pipeline.Options{
		Registry:    spec.Registry,
		Scope:       model.PipelineScope(pl, p.Project, p.Env, spec.Params),
		Environment: envName,
		Observer:    spec.Observer,
		RunID:       spec.RunID,
		Rows:        spec.Rows,
	})
	if err == nil {
		// Warnings of the schemas, e.g. fields that are not known (ADR 0007).
		p.Warnings = append(p.Warnings, p.Runner.Warnings()...)
		p.Warnings.Sort()
	}
	return p, err
}
