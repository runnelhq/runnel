package model

import (
	"strings"
	"testing"
)

const clipWorkflow = `kind: Workflow
apiVersion: runnel/v1
name: nightly
actions:
  - id: start
    type: core.start
  # the import
  - id: import
    type: core.run-pipeline
    with:
      path: pipelines/orders.pipeline.yaml

  - id: report
    type: core.log
    with:
      message: "imported {{ results.import.output.errors }} rows"
      level: |
        info
links:
  - { from: start, to: import }
  - from: import
    to: report
    when: "{{ results.import.status == 'ok' }}"
layout:
  start: { x: 0, y: 0 }
  import: { x: 240, y: 0 }
  report: { x: 480, y: 0 }
`

// TestCopyWorkflow: the copied actions as written (comments before an item and blank
// lines after it left out), the links between them and their positions.
func TestCopyWorkflow(t *testing.T) {
	c, err := CopyWorkflow("workflows/nightly.workflow.yaml", []byte(clipWorkflow), []string{"report", "import"})
	if err != nil {
		t.Fatal(err)
	}
	want := `# Runnel: 2 actions and 1 link from workflows/nightly.workflow.yaml
actions:
  - id: import
    type: core.run-pipeline
    with:
      path: pipelines/orders.pipeline.yaml
  - id: report
    type: core.log
    with:
      message: "imported {{ results.import.output.errors }} rows"
      level: |
        info
links:
  - from: import
    to: report
    when: "{{ results.import.status == 'ok' }}"
layout:
  import: { x: 240, y: 0 }
  report: { x: 480, y: 0 }
`
	if c.Text != want {
		t.Errorf("text:\n%s\nwant:\n%s", c.Text, want)
	}
	if c.Steps != 2 || c.Links != 1 || c.Omitted != 1 {
		t.Errorf("counts = %+v, want 2 steps, 1 link, 1 omitted", c)
	}
	if _, err := CopyWorkflow("w.workflow.yaml", []byte(clipWorkflow), []string{"nope"}); err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("unknown id: err = %v", err)
	}
	if _, err := CopyWorkflow("w.workflow.yaml", []byte(clipWorkflow), nil); err == nil {
		t.Error("nothing selected: no error")
	}
}

// TestPasteWorkflowSameFile: pasted into the file it was copied from, the actions get new
// ids, the expressions among them follow (with brackets for ids with "-", in the quotes
// that fit the line) and the positions move a little.
func TestPasteWorkflowSameFile(t *testing.T) {
	c, err := CopyWorkflow("w.workflow.yaml", []byte(clipWorkflow), []string{"import", "report"})
	if err != nil {
		t.Fatal(err)
	}
	out, res, err := PasteWorkflow("w.workflow.yaml", []byte(clipWorkflow), Paste{Text: c.Text})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(clipWorkflow, `links:
`, `  - id: import-2
    type: core.run-pipeline
    with:
      path: pipelines/orders.pipeline.yaml
  - id: report-2
    type: core.log
    with:
      message: "imported {{ results['import-2'].output.errors }} rows"
      level: |
        info
links:
`, 1)
	want = strings.Replace(want, `layout:
`, `  - { from: import-2, to: report-2, when: "{{ results[\"import-2\"].status == 'ok' }}" }
layout:
`, 1) + "  import-2: { x: 280, y: 40 }\n  report-2: { x: 520, y: 40 }\n"
	if string(out) != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	if strings.Join(res.IDs, ",") != "import-2,report-2" || res.Links != 1 || res.References != 2 ||
		res.Renamed["import"] != "import-2" || res.Renamed["report"] != "report-2" {
		t.Errorf("result = %+v", res)
	}
}

// TestPasteWorkflowElsewhere: free ids stay, the position follows At, CRLF stays.
func TestPasteWorkflowElsewhere(t *testing.T) {
	target := "kind: Workflow\r\napiVersion: runnel/v1\r\nname: other\r\nactions:\r\n  - id: begin\r\n    type: core.start\r\n"
	clip := "actions:\n  - id: say\n    type: core.log\n    with: { message: \"{{ results.say.status }}\" }\nlayout:\n  say: { x: 500, y: 300 }\n"
	out, res, err := PasteWorkflow("w.workflow.yaml", []byte(target), Paste{Text: clip, At: &Point{X: 10, Y: 20}})
	if err != nil {
		t.Fatal(err)
	}
	want := target + "  - id: say\r\n    type: core.log\r\n    with: { message: \"{{ results.say.status }}\" }\r\nlayout:\r\n  say: { x: 10, y: 20 }\r\n"
	if string(out) != want {
		t.Errorf("got:\n%q\nwant:\n%q", out, want)
	}
	if res.Renamed != nil || res.References != 0 || res.Links != 0 {
		t.Errorf("result = %+v", res)
	}
}

// TestPastePipeline: targets of core.switch follow renamed transforms; links to
// transforms that were not pasted are left out; numbered ids count on.
func TestPastePipeline(t *testing.T) {
	target := `kind: Pipeline
apiVersion: runnel/v1
name: p
transforms:
  - id: route
    type: core.dummy
  - id: sink-2
    type: core.dummy
`
	clip := `transforms:
  - id: route
    type: core.switch
    with:
      cases:
        - { when: "{{ record.n > 1 }}", to: sink-2 }
        - when: "{{ record.n == 1 }}"
          to: "other"
  - id: sink-2
    type: core.dummy
links:
  - { from: route, to: sink-2 }
  - { from: route, to: other, type: error }
`
	out, res, err := PastePipeline("p.pipeline.yaml", []byte(target), Paste{Text: clip})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"  - id: route-2\n", "to: sink-3 }", `to: "other"`, "  - id: sink-3\n", "  - { from: route-2, to: sink-3 }\n"} {
		if !strings.Contains(string(out), s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}
	if res.References != 1 || res.Links != 1 {
		t.Errorf("result = %+v", res)
	}
}

// With the target fields of the types (from their schemas), else of core.switch and
// else of core.filter follow renamed ids too; a field that is not a target stays.
func TestPastePipelineTargets(t *testing.T) {
	target := "kind: Pipeline\napiVersion: runnel/v1\nname: p\ntransforms:\n  - id: rest\n    type: core.dummy\n"
	clip := `transforms:
  - id: route
    type: core.switch
    with:
      cases:
        - { when: "{{ record.n > 1 }}", to: rest }
      else: rest
  - { id: check, type: core.filter, with: { condition: "{{ true }}", else: rest } }
  - { id: names, type: core.map-values, with: { field: x, default: rest } }
  - id: rest
    type: core.dummy
`
	targets := func(typ string) [][]string {
		switch typ {
		case "core.switch":
			return [][]string{{"cases", "*", "to"}, {"else"}}
		case "core.filter":
			return [][]string{{"else"}}
		}
		return nil
	}
	out, res, err := PastePipeline("p.pipeline.yaml", []byte(target), Paste{Text: clip, Targets: targets})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"to: rest-2 }", "      else: rest-2\n", "else: rest-2 }", "with: { field: x, default: rest }", "  - id: rest-2\n"} {
		if !strings.Contains(string(out), s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}
	if res.References != 3 {
		t.Errorf("references %d", res.References)
	}
}

// TestPasteRejected: the wrong kind of steps, text without steps, too much text, an
// item without id.
func TestPasteRejected(t *testing.T) {
	pipeline := "kind: Pipeline\napiVersion: runnel/v1\nname: p\ntransforms:\n  - id: a\n    type: core.dummy\n"
	for _, tt := range []struct{ name, text, want string }{
		{"actions into a pipeline", "actions:\n  - id: x\n    type: core.log\n", "Actions can't be pasted into a pipeline"},
		{"no steps", "hello world", "the clipboard holds no actions or transforms"},
		{"no YAML", "a: [", "the clipboard holds no actions or transforms"},
		{"no id", "transforms:\n  - type: core.dummy\n", "missing id"},
		{"too large", "transforms:\n" + strings.Repeat("#", MaxPasteSize), "too large"},
		{"bad link", "transforms:\n  - id: x\n    type: core.dummy\nlinks:\n  - { from: x, to: x, type: loop }\n", "not supported"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := PastePipeline("p.pipeline.yaml", []byte(pipeline), Paste{Text: tt.text})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
	_, _, err := PasteWorkflow("w.workflow.yaml", []byte(clipWorkflow), Paste{Text: "transforms:\n  - id: x\n    type: core.dummy\n"})
	if err == nil || !strings.Contains(err.Error(), "Transforms can't be pasted into a workflow") {
		t.Errorf("transforms into a workflow: err = %v", err)
	}
}

func TestRenameResults(t *testing.T) {
	renamed := map[string]string{"a": "a-2", "b": "b_2"}
	for _, tt := range []struct{ in, want string }{
		{`message: "{{ results.a.status }}"`, `message: "{{ results['a-2'].status }}"`},
		{`message: {{ results.a.status }}`, `message: {{ results["a-2"].status }}`},
		{`message: '{{ results["a"].output }} and {{ results.b }}'`, `message: '{{ results["a-2"].output }} and {{ results.b_2 }}'`},
		{`message: "results.a {{ results.ab }} {{ results.c }}"`, `message: "results.a {{ results.ab }} {{ results.c }}"`},
	} {
		if got, _ := renameResults(tt.in, renamed, 0); got != tt.want {
			t.Errorf("renameResults(%s) = %s, want %s", tt.in, got, tt.want)
		}
	}
}
