package model

import "testing"

// The JavaScript of core.script is code: ${…} and {{ … }} in it are neither checked as
// expressions nor found as variables (ADR 0028); params and other actions still are.
func TestCodeFields(t *testing.T) {
	src := `kind: Workflow
apiVersion: runnel/v1
name: js
actions:
  - id: js
    type: core.script
    with:
      params: { a: "${A}" }
      script: "return ` + "`${params.a}`" + ` + '{{ not valid( }}'"
  - id: ps
    type: shell.powershell
    with:
      script: "${B}"
`
	if _, err := ReadWorkflow("w.workflow.yaml", []byte(src)); err != nil {
		t.Fatal(err)
	}
	uses, _, err := ScanVariables("w.workflow.yaml", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, u := range uses {
		names = append(names, u.Name)
	}
	if len(names) != 2 || names[0] != "A" || names[1] != "B" {
		t.Errorf("variables %v", names)
	}
}

// In a pipeline, script, init and end of the transform core.script are code too (S73b).
func TestCodeFieldsPipeline(t *testing.T) {
	src := `kind: Pipeline
apiVersion: runnel/v1
name: js
transforms:
  - id: js
    type: core.script
    with:
      params: { a: "${A}" }
      init: "let x = ` + "`${y}`" + `"
      script: "return ` + "`${params.a}`" + ` + '{{ not valid( }}'"
      end: "return { t: '${Z}' }"
`
	if _, err := ReadPipeline("p.pipeline.yaml", []byte(src)); err != nil {
		t.Fatal(err)
	}
	uses, _, err := ScanVariables("p.pipeline.yaml", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(uses) != 1 || uses[0].Name != "A" {
		t.Errorf("variables %v", uses)
	}
}
