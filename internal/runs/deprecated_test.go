package runs

import (
	"context"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/transforms"
)

// Nothing built in is deprecated yet (ADR 0029); the test types rename "overwrite" to
// "ifExists" (action) and "maxRows" to "maxRecords" (transform).

func deprecatedActions() *action.Registry {
	r := actions.Builtin()
	r.Register(action.Definition{Type: "test.copy", Category: action.CategoryFiles, Label: "Copy", Description: "Test",
		Config: struct {
			IfExists string `json:"ifExists" doc:"If the target exists" schema:"enum=replace|fail"`
		}{},
		Deprecated: []action.Deprecated{{Field: "overwrite", RenamedTo: "ifExists", Use: "ifExists: replace", RemovedIn: "2.0"}},
		New: func(with map[string]any) (action.Action, error) {
			var c struct {
				IfExists string `json:"ifExists"`
			}
			if err := action.DecodeConfig(with, &c); err != nil {
				return nil, err
			}
			return action.Func(func(context.Context, *action.ExecContext) (action.Result, error) { return action.Result{}, nil }), nil
		}})
	return r
}

const deprecatedWorkflow = `kind: Workflow
apiVersion: runnel/v1
name: copy
actions:
  - id: start
    type: core.start
  - id: copy
    type: test.copy
    with:
      overwrite: replace
links:
  - { from: start, to: copy }
`

func TestDeprecatedFieldInWorkflow(t *testing.T) {
	check := func(text string) model.Errors {
		t.Helper()
		w, err := model.ReadWorkflow("w.workflow.yaml", []byte(text))
		if err != nil {
			t.Fatal(err)
		}
		diags, err := CheckWorkflow(w, w.Validate(), WorkflowCheck{Registry: deprecatedActions()})
		if err != nil {
			t.Fatal(err)
		}
		return diags
	}
	// The old field is a warning at its position; the workflow is valid and runs.
	diags := check(deprecatedWorkflow)
	want := `w.workflow.yaml:10:18: warning: action "copy": field "overwrite" is deprecated: use ifExists: replace (removed in 2.0)`
	if diags.HasErrors() || diags.Error() != want {
		t.Errorf("got  %s\nwant %s", diags.Error(), want)
	}
	p, err := Prepare(Spec{Name: "w.workflow.yaml", Data: []byte(deprecatedWorkflow), Registry: deprecatedActions()})
	if err != nil {
		t.Fatal(err)
	}
	if p.Warnings.Error() != want { // runnel run prints them
		t.Errorf("run warnings: %s", p.Warnings.Error())
	}
	if res := p.Runner.Run(t.Context()); res.Err != nil {
		t.Errorf("run: %v", res.Err)
	}
	// Old and new field together are an error.
	diags = check(strings.Replace(deprecatedWorkflow, "overwrite: replace", "overwrite: replace\n      ifExists: fail", 1))
	if !diags.HasErrors() || !strings.Contains(diags.Error(), `action "copy" (test.copy): fields "overwrite" and "ifExists" cannot be used together`) {
		t.Errorf("both: %s", diags.Error())
	}
	// The new name gives no warning.
	if diags := check(strings.Replace(deprecatedWorkflow, "overwrite", "ifExists", 1)); len(diags) != 0 {
		t.Errorf("new name: %s", diags.Error())
	}
}

func TestDeprecatedFieldInPipeline(t *testing.T) {
	reg := transforms.Builtin()
	toResult, _ := reg.Lookup("core.to-result")
	toResult.Type, toResult.Deprecated = "test.to-result", []action.Deprecated{{Field: "maxRows", RenamedTo: "maxRecords", RemovedIn: "2.0"}}
	reg.Register(toResult)
	text := `kind: Pipeline
apiVersion: runnel/v1
name: p
transforms:
  - id: rows
    type: core.generate-rows
    with:
      count: 1
  - id: result
    type: test.to-result
    with:
      maxRows: 10
links:
  - { from: rows, to: result }
`
	check := func(text string) model.Errors {
		t.Helper()
		p, err := model.ReadPipeline("p.pipeline.yaml", []byte(text))
		if err != nil {
			t.Fatal(err)
		}
		return CheckPipeline(p, p.Validate(), reg, nil)
	}
	want := `p.pipeline.yaml:12:16: warning: transform "result": field "maxRows" is deprecated: use maxRecords (removed in 2.0)`
	if diags := check(text); diags.HasErrors() || diags.Error() != want {
		t.Errorf("got  %s\nwant %s", diags.Error(), want)
	}
	diags := check(strings.Replace(text, "maxRows: 10", "maxRows: 10\n      maxRecords: 5", 1))
	if !diags.HasErrors() || !strings.Contains(diags.Error(), `fields "maxRows" and "maxRecords" cannot be used together`) {
		t.Errorf("both: %s", diags.Error())
	}
	pp, err := PreparePipeline(PipelineSpec{Name: "p.pipeline.yaml", Data: []byte(text), Registry: reg})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pp.Warnings.Error(), want) {
		t.Errorf("run warnings: %s", pp.Warnings.Error())
	}
}
