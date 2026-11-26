package runs

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/engine/pipeline"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/transforms"
)

func scriptWorkflow(with string) string {
	return `kind: Workflow
apiVersion: runnel/v1
name: js
parameters:
  - name: FOLDER
    default: data
actions:
  - id: start
    type: core.start
  - id: js
    type: core.script
    with:
` + with + `
outputs:
  - name: OUT
    value: "{{ results.js.output.token }}"
links:
  - { from: start, to: js }
`
}

const scriptProject = "kind: Project\napiVersion: runnel/v1\nname: t\nsecretStore: env\n"

func prepareScript(t *testing.T, with string) (*Prepared, *recorder, error) {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "project.yaml"), scriptProject)
	rec := &recorder{}
	p, err := Prepare(Spec{Name: "workflows/js.workflow.yaml", Data: []byte(scriptWorkflow(with)), ProjectDir: dir,
		Registry: actions.Builtin(), Observer: rec.observe})
	return p, rec, err
}

// In the JavaScript of core.script, ${…} and {{ … }} are code (ADR 0028): neither
// checked nor replaced. params take variables, expressions and – as a value of its
// own – a secret, whose value is masked in log, output and error.
func TestScriptAction(t *testing.T) {
	t.Setenv("RUNNEL_TEST_TOKEN", "T0ken-s3cret")
	with := `      params:
        folder: ${FOLDER}
        upper: "{{ upper(params.FOLDER) }}"
        token: ${secret:RUNNEL_TEST_TOKEN}
      script: |
        console.log(` + "`${params.folder} ${params.upper} {{ x }} ${params.token}`" + `);
        return { token: "t:" + params.token };`
	w, err := model.ReadWorkflow("w.workflow.yaml", []byte(scriptWorkflow(with)))
	if err != nil {
		t.Fatal(err)
	}
	diags, err := CheckWorkflow(w, nil, WorkflowCheck{Variables: true, Project: &model.Project{SecretStore: "env"}, Registry: actions.Builtin(), Params: map[string]string{}})
	if err != nil || len(diags) > 0 {
		t.Fatalf("check: %v %v", diags, err)
	}

	p, rec, err := prepareScript(t, with)
	if err != nil {
		t.Fatal(err)
	}
	res := p.Runner.Run(context.Background())
	if res.Status != workflow.Succeeded {
		t.Fatalf("%s: %v", res.Status, res.Err)
	}
	logs := rec.logs()
	if !strings.Contains(logs, "data DATA {{ x }} ***") || strings.Contains(logs, "T0ken-s3cret") {
		t.Errorf("log:\n%s", logs)
	}
	if res.Outputs["OUT"] != "t:***" {
		t.Errorf("outputs %v", res.Outputs)
	}

	// An error with the secret shows it masked.
	p, _, err = prepareScript(t, strings.Replace(with, "return {", "throw new Error('bad ' + params.token);\n        return {", 1))
	if err != nil {
		t.Fatal(err)
	}
	res = p.Runner.Run(context.Background())
	if res.Status != workflow.Failed || !strings.Contains(res.Err.Error(), "bad ***") || strings.Contains(res.Err.Error(), "T0ken-s3cret") {
		t.Errorf("%s: %v", res.Status, res.Err)
	}
}

// A secret in params must be a value of its own; a missing one stops the run before it
// starts. The file of a script takes no ${…}.
func TestScriptActionChecks(t *testing.T) {
	for with, want := range map[string]string{
		"      params:\n        token: ${secret:RUNNEL_TEST_MISSING}\n      script: return 1":               `secret "RUNNEL_TEST_MISSING": environment variable RUNNEL_TEST_MISSING is not set`,
		"      params:\n        deep: { token: \"${secret:RUNNEL_TEST_MISSING}\" }\n      script: return 1": "is not allowed here",
		"      file: scripts/${FOLDER}.js": "the script file must not contain ${…} or {{ … }}",
	} {
		_, _, err := prepareScript(t, with)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", with, err, want)
		}
	}
}

func scriptPipeline(with string) string {
	return `kind: Pipeline
apiVersion: runnel/v1
name: js
parameters:
  - name: LABEL
    default: total
transforms:
  - { id: gen, type: core.generate-rows, with: { count: 1000, numberField: n } }
  - id: js
    type: core.script
    copies: 4
    with:
` + with + `
  - { id: out, type: core.to-result }
links:
  - { from: gen, to: js }
  - { from: js, to: out }
`
}

func runScriptPipeline(t *testing.T, dir, with string) (pipeline.Result, string, error) {
	t.Helper()
	var mu sync.Mutex
	var logs strings.Builder
	p, err := PreparePipeline(PipelineSpec{Name: "pipelines/js.pipeline.yaml", Data: []byte(scriptPipeline(with)), ProjectDir: dir,
		Registry: transforms.Builtin(), Observer: func(e pipeline.Event) {
			mu.Lock()
			defer mu.Unlock()
			if e.Kind == pipeline.Log {
				logs.WriteString(e.Message + "\n")
			}
		}})
	if err != nil {
		return pipeline.Result{}, "", err
	}
	res := p.Runner.Run(context.Background())
	mu.Lock()
	defer mu.Unlock()
	return res, logs.String(), nil
}

// The transform core.script in a pipeline (S73b): each copy has its own runtime (init and
// end run per copy); ${…} and {{ … }} in the code are text, params take variables and
// secrets, whose values are masked; modules come from scripts/ of the project.
func TestScriptTransform(t *testing.T) {
	t.Setenv("RUNNEL_TEST_TOKEN", "T0ken-s3cret")
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "project.yaml"), scriptProject)
	writeTestFile(t, filepath.Join(dir, "scripts", "util.js"), "exports.double = x => 2 * x;")
	with := `      params:
        label: ${LABEL}
        token: ${secret:RUNNEL_TEST_TOKEN}
      init: |
        const util = require('./scripts/util.js');
        let count = 0;
      script: |
        count++;
        if (record.n === 1) console.log(` + "`${params.label} {{ x }} ${params.token}`" + `);
        return { n: record.n, double: util.double(record.n) };
      end: |
        return { label: params.label, count };`
	p, err := model.ReadPipeline("js.pipeline.yaml", []byte(scriptPipeline(with)))
	if err != nil {
		t.Fatal(err)
	}
	if diags := CheckPipeline(p, nil, transforms.Builtin(), &model.Project{Dir: dir, SecretStore: "env"}); len(diags) > 0 {
		t.Fatalf("check: %v", diags)
	}
	res, logs, err := runScriptPipeline(t, dir, with)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != pipeline.Succeeded {
		t.Fatalf("%s: %v", res.Status, res.Err)
	}
	if !strings.Contains(logs, "total {{ x }} ***") || strings.Contains(logs, "T0ken-s3cret") {
		t.Errorf("log:\n%s", logs)
	}
	records, totals, copies := 0, int64(0), 0
	for _, r := range res.Rows {
		if c, ok := r.Get("count"); ok {
			copies++
			totals += c.(int64)
			continue
		}
		if n, _ := r.Get("n"); r.Len() == 2 {
			if d, _ := r.Get("double"); d != 2*n.(int64) {
				t.Errorf("record %v", r.Fields())
			}
			records++
		}
	}
	if records != 1000 || copies != 4 || totals != 1000 {
		t.Errorf("%d records, %d copies, %d counted", records, copies, totals)
	}

	// Without an error link, an exception stops the pipeline; the secret is masked.
	res, _, err = runScriptPipeline(t, dir, strings.Replace(with, "count++;", "if (record.n === 500) throw new Error('bad ' + params.token);", 1))
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != pipeline.Failed || !strings.Contains(res.Err.Error(), "record ") || !strings.Contains(res.Err.Error(), "script, line 1: bad ***") ||
		strings.Contains(res.Err.Error(), "T0ken-s3cret") {
		t.Errorf("%s: %v", res.Status, res.Err)
	}
}

// A secret in params must be a value of its own; a missing one stops the run before it
// starts. The file of a script takes no ${…}.
func TestScriptTransformChecks(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "project.yaml"), scriptProject)
	for with, want := range map[string]string{
		"      params:\n        token: ${secret:RUNNEL_TEST_MISSING}\n      script: return record": `RUNNEL_TEST_MISSING is not set`,
		"      file: scripts/${LABEL}.js": "the script file must not contain ${…} or {{ … }}",
	} {
		if _, _, err := runScriptPipeline(t, dir, with); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", with, err, want)
		}
	}
	p, err := model.ReadPipeline("js.pipeline.yaml", []byte(scriptPipeline("      params:\n        deep: { token: \"${secret:RUNNEL_TEST_MISSING}\" }\n      script: return record")))
	if err != nil {
		t.Fatal(err)
	}
	if diags := CheckPipeline(p, nil, transforms.Builtin(), &model.Project{Dir: dir, SecretStore: "env"}); !strings.Contains(diags.Error(), "not allowed here") {
		t.Errorf("deep secret: %v", diags)
	}
}
