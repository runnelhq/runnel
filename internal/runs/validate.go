package runs

import (
	"strings"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/engine/pipeline"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/schema"
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
	c.Registry = WithPlugins(c.Registry, c.ProjectDir) // actions of plugins (S49b2)
	if c.Variables {
		contexts, err := model.VarContexts(w, c.Project, c.Env, c.Params)
		if err != nil {
			return nil, err
		}
		diags = append(diags, w.CheckVariables(contexts)...)
	}
	var stores []string // with the project known, the default stores are checked too
	if c.Variables {
		stores = model.SecretStoresOf(c.Project, c.Env)
	}
	diags = append(diags, w.CheckSecrets(ActionSecretField(c.Registry), stores)...)
	diags = append(diags, w.CheckSQL(ActionSQLField(c.Registry))...)
	diags = append(diags, w.CheckProgram(ActionProgramField(c.Registry))...)
	diags = append(diags, w.CheckScript(ActionScriptField(c.Registry))...)
	if c.Variables && c.Project != nil {
		diags = append(diags, checkConnectionRefs(workflowSteps(w, c.Registry), c.Project)...)
	}
	if !diags.HasErrors() {
		diags = append(diags, workflow.Check(w, workflow.Options{Registry: c.Registry})...)
		diags = append(diags, CheckCalls(w, c.ProjectDir, c.File)...)
	}
	diags = append(diags, CheckOnError(w.ErrorAt, w.WarningAt, w.Settings.OnError, c.ProjectDir, c.File)...)
	diags.Sort()
	return diags, nil
}

// CheckPipeline checks a pipeline beyond its fields and graph, like before a run: the
// types, configurations and fields of its transforms (ADR 0007), only if diags (the
// problems found so far) has no errors and registry is set. The result is sorted.
func CheckPipeline(p *model.Pipeline, diags model.Errors, registry *transform.Registry, project *model.Project) model.Errors {
	if project != nil {
		registry = WithPluginTransforms(registry, project.Dir) // transforms of plugins (S50a)
	}
	diags = append(diags, p.CheckSecrets(TransformSecretField(registry), nil)...)
	diags = append(diags, p.CheckSQL(TransformSQLField(registry))...)
	if project != nil {
		diags = append(diags, checkConnectionRefs(pipelineSteps(p, registry), project)...)
		diags = append(diags, CheckOnError(p.ErrorAt, p.WarningAt, p.Settings.OnError, project.Dir, "")...)
	}
	if !diags.HasErrors() && registry != nil {
		diags = append(diags, pipeline.Check(p, pipeline.Options{Registry: registry})...)
	}
	diags.Sort()
	return diags
}

// ActionSecretField tells whether a field of an action type holds a secret (format
// runnel-secret, ADR 0022): only such fields may use ${secret:…}.
func ActionSecretField(reg *action.Registry) func(typ, field string) bool {
	return func(typ, field string) bool {
		if reg == nil {
			return false
		}
		d, ok := reg.Descriptor(typ)
		return ok && secretField(d.Schema, field)
	}
}

// TransformSecretField is ActionSecretField for transforms.
func TransformSecretField(reg *transform.Registry) func(typ, field string) bool {
	return func(typ, field string) bool {
		if reg == nil {
			return false
		}
		d, ok := reg.Descriptor(typ)
		return ok && secretField(d.Schema, field)
	}
}

func secretField(s *schema.Schema, field string) bool {
	if s == nil {
		return false
	}
	// A value in a mapping of secrets, e.g. env.TOKEN of shell.run.
	if i := strings.IndexAny(field, ".["); i >= 0 {
		field = field[:i]
	}
	for _, p := range s.Properties {
		if p.Name == field {
			return p.Schema.Format == schema.FormatSecret
		}
	}
	return false
}
