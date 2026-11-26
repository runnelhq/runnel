package runs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/engine/workflow"
)

const childPipeline = `kind: Pipeline
apiVersion: runnel/v1
name: numbers
parameters:
  - { name: ROWS, type: integer, required: true }
  - { name: MIN, type: integer, default: "0" }
transforms:
  - { id: gen, type: core.generate-rows, with: { count: "${ROWS}", numberField: n } }
  - { id: check, type: core.filter, with: { condition: "{{ record.n > params.MIN }}" } }
  - { id: show, type: core.log, with: { every: 1000 } }
links:
  - { from: gen, to: check }
  - { from: check, to: show }
`

func pipelineParent(with string) string {
	return `kind: Workflow
apiVersion: runnel/v1
name: parent
parameters:
  - { name: TARGET, default: pipelines/numbers.pipeline.yaml }
outputs:
  - { name: KEPT, type: integer, value: "{{ results.load.output.transforms.check.written }}" }
  - { name: ERRORS, type: integer, value: "{{ results.load.output.errors }}" }
actions:
  - { id: start, type: core.start }
  - id: load
    type: core.run-pipeline
    with:
` + with + `
links:
  - { from: start, to: load }
`
}

func TestRunPipelineCall(t *testing.T) {
	dir := project(t, map[string]string{
		"pipelines/numbers.pipeline.yaml": childPipeline,
		"workflows/parent.workflow.yaml": pipelineParent(`      pipeline: pipelines/numbers.pipeline.yaml
      params: { ROWS: "{{ 10 * 10 }}", MIN: 40 }`),
	})
	res, rec, err := start(t, dir, nil)
	if err != nil || res.Status != workflow.Succeeded {
		t.Fatalf("status %s: %v %v\n%s", res.Status, err, res.Err, rec.logs())
	}
	if res.Outputs["KEPT"] != int64(60) || res.Outputs["ERRORS"] != int64(0) {
		t.Errorf("outputs %v", res.Outputs)
	}
	logs := rec.logs()
	for _, want := range []string{
		`DEBUG [load] pipeline "numbers" started, run `,
		"INFO [load] numbers/check: kept 60 of 100 records, dropped 40",
		"DEBUG [load] numbers/check: read 100, written 60 in ",
		`INFO [load] pipeline "numbers" succeeded in `,
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("missing %q in log:\n%s", want, logs)
		}
	}
}

func TestRunPipelineCallFails(t *testing.T) {
	dir := project(t, map[string]string{
		"pipelines/numbers.pipeline.yaml": childPipeline,
		"pipelines/broken.pipeline.yaml":  strings.Replace(childPipeline, "core.filter", "core.flter", 1),
		"workflows/parent.workflow.yaml": pipelineParent(`      pipeline: "${TARGET}"
      params: { ROWS: "{{ params.TARGET == 'pipelines/numbers.pipeline.yaml' ? 'many' : '5' }}" }`),
	})
	for _, tt := range []struct{ target, want string }{
		// Paths with variables are checked when the action runs.
		{"pipelines/missing.pipeline.yaml", "pipeline file pipelines/missing.pipeline.yaml not found"},
		{"pipelines/broken.pipeline.yaml", `pipeline pipelines/broken.pipeline.yaml cannot be started: pipelines/broken.pipeline.yaml:9:24: transform "check": unknown transform type "core.flter"`},
		// A wrong value fails the pipeline, and with it the action.
		{"pipelines/numbers.pipeline.yaml", "parameter ROWS"},
	} {
		res, rec, err := start(t, dir, map[string]string{"TARGET": tt.target})
		if err != nil || res.Status != workflow.Failed || !strings.Contains(res.Err.Error(), tt.want) {
			t.Errorf("%s: %v %v, want containing %q\n%s", tt.target, err, res.Err, tt.want, rec.logs())
		}
	}
	// Undeclared parameters are an error, like for called workflows.
	dir = project(t, map[string]string{
		"pipelines/numbers.pipeline.yaml": childPipeline,
		"workflows/parent.workflow.yaml": pipelineParent(`      pipeline: "${TARGET}"
      params: { ROWS: 5, EXTRA: "{{ 1 }}" }`),
	})
	res, _, _ := start(t, dir, nil)
	if res.Err == nil || !strings.Contains(res.Err.Error(), `pipeline pipelines/numbers.pipeline.yaml cannot be started: parameter(s) [EXTRA] not declared by pipeline "numbers" (declared: ROWS, MIN)`) {
		t.Errorf("undeclared: %v", res.Err)
	}
}

func TestRunPipelineCallCancelled(t *testing.T) {
	dir := project(t, map[string]string{
		"pipelines/numbers.pipeline.yaml": childPipeline,
		"workflows/parent.workflow.yaml": pipelineParent(`      pipeline: pipelines/numbers.pipeline.yaml
      params: { ROWS: 1000000000 }`),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	res, _, err := startCtx(ctx, t, dir, nil)
	if err != nil || res.Status != workflow.Cancelled {
		t.Errorf("status %s: %v %v", res.Status, err, res.Err)
	}
}

func TestCheckPipelineCalls(t *testing.T) {
	dir := project(t, map[string]string{
		"pipelines/numbers.pipeline.yaml": childPipeline,
		"pipelines/bad.pipeline.yaml":     "kind: Pipeline\napiVersion: runnel/v1\nname: bad\ntransforms: []\nnope: 1\n",
		"workflows/parent.workflow.yaml": `kind: Workflow
apiVersion: runnel/v1
name: parent
actions:
  - { id: start, type: core.start }
  - { id: a, type: core.run-pipeline, with: { pipeline: pipelines/numbers.pipeline.yaml, params: { MIN: x, ROWZ: 1 } } }
  - { id: b, type: core.run-pipeline, with: { pipeline: pipelines/missing.pipeline.yaml } }
  - { id: c, type: core.run-pipeline, with: { pipeline: pipelines/bad.pipeline.yaml } }
  - { id: d, type: core.run-pipeline, with: { pipeline: "pipelines/${X}.pipeline.yaml" } }
links:
  - { from: start, to: a }
  - { from: a, to: b }
  - { from: b, to: c }
  - { from: c, to: d }
`,
	})
	_, _, err := start(t, dir, nil)
	got := ""
	if err != nil {
		got = err.Error()
	}
	for _, want := range []string{
		`action "a": missing required parameter ROWS of pipeline pipelines/numbers.pipeline.yaml`,
		`action "a": parameter MIN (integer): `,
		`action "a": parameter ROWZ is not declared by pipeline pipelines/numbers.pipeline.yaml (declared: ROWS, MIN)`,
		`action "b": pipeline file pipelines/missing.pipeline.yaml not found`,
		`action "c": pipeline file pipelines/bad.pipeline.yaml has errors; check it with "runnel validate"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// A path with a variable is checked when the action runs; it only gets a warning.
	if !strings.Contains(got, `warning: action "d": the pipeline is only known when the action runs; restrict it with allowedTargets`) ||
		strings.Count(got, `action "d"`) != 1 {
		t.Errorf("a path with a variable is checked when the action runs:\n%s", got)
	}
}

func TestRunPipelineCallWithRows(t *testing.T) {
	dir := project(t, map[string]string{
		"pipelines/double.pipeline.yaml": `kind: Pipeline
apiVersion: runnel/v1
name: double
transforms:
  - { id: in, type: core.from-result }
  - { id: big, type: core.filter, with: { condition: "{{ record.size > 10 }}" } }
  - { id: out, type: core.to-result }
links:
  - { from: in, to: big }
  - { from: big, to: out }
`,
		"workflows/parent.workflow.yaml": `kind: Workflow
apiVersion: runnel/v1
name: parent
outputs:
  - { name: COUNT, type: integer, value: "{{ len(results.load.output.records) }}" }
  - { name: FIRST, value: "{{ results.load.output.records[0].name }}" }
actions:
  - { id: start, type: core.start }
  - id: load
    type: core.run-pipeline
    with:
      pipeline: pipelines/double.pipeline.yaml
      records: "{{ [{'name': 'a.csv', 'size': 5}, {'name': 'b.csv', 'size': 50}, {'size': 99, 'name': 'c.csv'}] }}"
links:
  - { from: start, to: load }
`,
	})
	res, rec, err := start(t, dir, nil)
	if err != nil || res.Status != workflow.Succeeded {
		t.Fatalf("status %s: %v %v\n%s", res.Status, err, res.Err, rec.logs())
	}
	if res.Outputs["COUNT"] != int64(2) || res.Outputs["FIRST"] != "b.csv" {
		t.Errorf("outputs %v", res.Outputs)
	}
}

func TestRunPipelineCallWithInvalidRows(t *testing.T) {
	dir := project(t, map[string]string{
		"pipelines/numbers.pipeline.yaml": childPipeline,
		"workflows/parent.workflow.yaml": pipelineParent(`      pipeline: pipelines/numbers.pipeline.yaml
      params: { ROWS: 1 }
      records: "{{ 'not a list' }}"`),
	})
	res, _, _ := start(t, dir, nil)
	if res.Err == nil || !strings.Contains(res.Err.Error(), "records must be a list, e.g. {{ results.files.output }}, not not a list") {
		t.Errorf("%v", res.Err)
	}
	// A list of plain values becomes records with the field value.
	rows, err := Records([]map[string]any{{"value": "x"}, {"b": 2, "a": 1}})
	if err != nil || len(rows) != 2 || strings.Join(rows[1].Names(), ",") != "a,b" {
		t.Errorf("%v %v", rows, err)
	}
	if _, err := Records([]map[string]any{{"x": struct{}{}}}); err == nil || !strings.Contains(err.Error(), "records[0].x") {
		t.Errorf("invalid value: %v", err)
	}
}

// A pipeline given by a variable stays in the project folder and in allowedTargets.
func TestRunPipelineCallTargets(t *testing.T) {
	dir := project(t, map[string]string{
		"pipelines/numbers.pipeline.yaml": childPipeline,
		"workflows/parent.workflow.yaml": `kind: Workflow
apiVersion: runnel/v1
name: parent
parameters:
  - { name: TARGET }
actions:
  - { id: start, type: core.start }
  - id: run
    type: core.run-pipeline
    with:
      pipeline: "${TARGET}"
      allowedTargets: ["pipelines/*.pipeline.yaml", "../*.pipeline.yaml"]
      params: { ROWS: "1" }
links:
  - { from: start, to: run }
`,
	})
	for target, want := range map[string]string{
		"pipelines/numbers.pipeline.yaml": "",
		"pipelines/../../x.pipeline.yaml": "pipeline file ../x.pipeline.yaml lies outside the project folder",
		"data/x.pipeline.yaml":            "pipeline data/x.pipeline.yaml is not one of the allowed targets",
	} {
		res, _, err := start(t, dir, map[string]string{"TARGET": target})
		switch {
		case err != nil:
			t.Errorf("%s: %v", target, err)
		case want == "" && res.Status != workflow.Succeeded:
			t.Errorf("%s: %v", target, res.Err)
		case want != "" && (res.Err == nil || !strings.Contains(res.Err.Error(), want)):
			t.Errorf("%s: %v, want %q", target, res.Err, want)
		}
	}
}
