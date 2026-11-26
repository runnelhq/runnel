package runs

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/model"
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
