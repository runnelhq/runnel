package model

import (
	"strings"
	"testing"
)

const editSource = `# Nightly import – comments must survive edits.
kind: Workflow
apiVersion: runnel/v1
name: edit
actions:
  # the entry point
  - id: start
    type: core.start
  - id: wait
    type: core.wait
    with:
      duration: 1s   # short
  - id: report
    type: core.log
    with:
      message: |
        line one
        line two
  - { id: done, type: core.success }
links:
  - { from: start, to: wait }
  - from: wait
    to: report
  - { from: report, to: done }   # end
layout:
  start:  { x: 40,  y: 120 }
  wait:   { x: 280, y: 120 }
  report:
    x: 520
    y: 120
`

func TestEditWorkflowMove(t *testing.T) {
	got := edit(t, editSource, WorkflowEdit{Move: map[string]Point{"wait": {X: 288, Y: 160}, "report": {X: 600, Y: 96}, "done": {X: 840, Y: 120}}})
	want := strings.Replace(editSource, "  wait:   { x: 280, y: 120 }\n", "  wait: { x: 288, y: 160 }\n", 1)
	want = strings.Replace(want, "  report:\n    x: 520\n    y: 120\n", "  report: { x: 600, y: 96 }\n  done: { x: 840, y: 120 }\n", 1)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditWorkflowDelete(t *testing.T) {
	got := edit(t, editSource, WorkflowEdit{Delete: []string{"report"}})
	want := strings.Replace(editSource, "  - id: report\n    type: core.log\n    with:\n      message: |\n        line one\n        line two\n", "", 1)
	want = strings.Replace(want, "  - from: wait\n    to: report\n", "", 1)
	want = strings.Replace(want, "  - { from: report, to: done }   # end\n", "", 1)
	want = strings.Replace(want, "  report:\n    x: 520\n    y: 120\n", "", 1)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if _, err := ParseWorkflow("x", []byte(got)); err != nil {
		t.Errorf("result does not parse: %v", err)
	}
}

func TestEditWorkflowKeepsComments(t *testing.T) {
	got := edit(t, editSource, WorkflowEdit{Delete: []string{"start"}})
	for _, c := range []string{"# Nightly import", "  # the entry point\n  - id: wait", "# short", "layout:\n  wait:"} {
		if !strings.Contains(got, c) {
			t.Errorf("missing %q in:\n%s", c, got)
		}
	}
}

func TestEditWorkflowCreatesLayout(t *testing.T) {
	src := "kind: Workflow\napiVersion: runnel/v1\nname: x\nactions:\n  - { id: start, type: core.start }\n  - { id: log, type: core.log, with: { message: hi } }\nlinks:\n  - { from: start, to: log }"
	got := edit(t, src, WorkflowEdit{Move: map[string]Point{"log": {X: 280, Y: 40}, "start": {X: 40, Y: 40}}})
	want := src + "\nlayout:\n  start: { x: 40, y: 40 }\n  log: { x: 280, y: 40 }\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}

	got = edit(t, src+"\nlayout: {}\n", WorkflowEdit{Move: map[string]Point{"start": {X: 0.5, Y: 8}}})
	if !strings.HasSuffix(got, "\nlayout:\n  start: { x: 0.5, y: 8 }\n") {
		t.Errorf("empty layout: got:\n%s", got)
	}
}

func TestEditWorkflowCRLF(t *testing.T) {
	src := strings.ReplaceAll(editSource, "\n", "\r\n")
	got := edit(t, src, WorkflowEdit{Move: map[string]Point{"start": {X: 0, Y: 0}}, Delete: []string{"done"}})
	if strings.Count(got, "\r\n") != strings.Count(got, "\n") {
		t.Errorf("line endings were mixed:\n%q", got)
	}
	if !strings.Contains(got, "  start: { x: 0, y: 0 }\r\n") || strings.Contains(got, "done") {
		t.Errorf("got:\n%q", got)
	}
}

func TestEditWorkflowErrors(t *testing.T) {
	for _, tt := range []struct {
		src  string
		edit WorkflowEdit
		want string
	}{
		{editSource, WorkflowEdit{Delete: []string{"nope"}}, `cannot delete action "nope"`},
		{editSource, WorkflowEdit{Move: map[string]Point{"nope": {}}}, `cannot move action "nope"`},
		{"kind: Workflow\nactions: [\n", WorkflowEdit{}, "syntax error"},
	} {
		if _, err := EditWorkflow("x.workflow.yaml", []byte(tt.src), tt.edit); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("error %v, want containing %q", err, tt.want)
		}
	}
}

func edit(t *testing.T, src string, e WorkflowEdit) string {
	t.Helper()
	out, err := EditWorkflow("x.workflow.yaml", []byte(src), e)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
