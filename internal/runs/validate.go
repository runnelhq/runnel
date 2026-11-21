package runs

import (
	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/engine/pipeline"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/transform"
)

// WorkflowCheck describes how CheckWorkflow checks a workflow ("runnel validate" and
// the editor).
type WorkflowCheck struct {
	// Variables checks the variables of the workflow against Project, Env and Params;
	// false if the project cannot be read.
	Variables bool
	Project   *model.Project
	Env       string
	Params    map[string]string

	Registry *action.Registry
	// ProjectDir and File locate the workflow for the calls of other workflows and
	// pipelines (CheckCalls).
	ProjectDir string
	File       string
}

// CheckWorkflow checks a workflow beyond its fields, like before a run: its variables,
// then – only on an otherwise valid workflow – the types and configurations of its
// actions and its calls of other workflows and pipelines. diags are the problems found
// so far (e.g. those of w.Validate); the result holds them and the new ones, sorted.
// The error is set for problems unrelated to the content (e.g. an unknown environment).
func CheckWorkflow(w *model.Workflow, diags model.Errors, c WorkflowCheck) (model.Errors, error) {
	if c.Variables {
		contexts, err := model.VarContexts(w, c.Project, c.Env, c.Params)
		if err != nil {
			return nil, err
		}
		diags = append(diags, w.CheckVariables(contexts)...)
	}
	if !diags.HasErrors() {
		diags = append(diags, workflow.Check(w, workflow.Options{Registry: c.Registry})...)
		diags = append(diags, CheckCalls(w, c.ProjectDir, c.File)...)
	}
	diags.Sort()
	return diags, nil
}

// CheckPipeline checks a pipeline beyond its fields and graph, like before a run: the
// types, configurations and fields of its transforms (ADR 0007), only if diags (the
// problems found so far) has no errors and registry is set. The result is sorted.
func CheckPipeline(p *model.Pipeline, diags model.Errors, registry *transform.Registry) model.Errors {
	if !diags.HasErrors() && registry != nil {
		diags = append(diags, pipeline.Check(p, pipeline.Options{Registry: registry})...)
	}
	diags.Sort()
	return diags
}
