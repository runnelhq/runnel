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
