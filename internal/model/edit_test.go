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
		{editSource, WorkflowEdit{Add: []NewAction{{ID: "wait", Type: "core.log"}}}, `cannot add action "wait": the id is already used`},
		{editSource, WorkflowEdit{Add: []NewAction{{ID: "1x", Type: "core.log"}}}, `invalid action id "1x"`},
		{editSource, WorkflowEdit{Add: []NewAction{{ID: "x", Type: "Log"}}}, `invalid action type "Log"`},
		{editSource, WorkflowEdit{Rename: map[string]string{"wait": "report"}}, `the id is already used`},
		{editSource, WorkflowEdit{Rename: map[string]string{"nope": "x"}}, `cannot rename action "nope"`},
		{editSource, WorkflowEdit{Rename: map[string]string{"wait": "x"}, Delete: []string{"done"}}, "cannot be combined"},
		{editSource, WorkflowEdit{AddLinks: []Link{{From: "wait", To: "nope"}}}, `action "nope" does not exist`},
		{editSource, WorkflowEdit{AddLinks: []Link{{From: "wait", To: "done", When: "{{ x }}"}}}, "not supported here"},
		{editSource, WorkflowEdit{DeleteLinks: []int{9}}, "cannot delete link #10"},
		{editSource, WorkflowEdit{UpdateLinks: []LinkUpdate{{Index: 0, When: "maybe"}}}, "not supported here"},
		{strings.Replace(editSource, "actions:\n", "links2: x\nactions:\n", 1), WorkflowEdit{}, "unknown field"},
		{"kind: Workflow\napiVersion: runnel/v1\nname: x\nactions: [{ id: a, type: core.start }]\n", WorkflowEdit{Add: []NewAction{{ID: "b", Type: "core.log"}}}, "flow style"},
	} {
		if _, err := EditWorkflow("x.workflow.yaml", []byte(tt.src), tt.edit); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("error %v, want containing %q", err, tt.want)
		}
	}
}

func TestEditWorkflowAdd(t *testing.T) {
	got := edit(t, editSource, WorkflowEdit{
		Add:      []NewAction{{ID: "log-1", Type: "core.log"}},
		Move:     map[string]Point{"log-1": {X: 760, Y: 120}},
		AddLinks: []Link{{From: "report", To: "log-1"}, {From: "wait", To: "log-1", When: "failure"}},
	})
	want := strings.Replace(editSource, "  - { id: done, type: core.success }\n",
		"  - { id: done, type: core.success }\n  - id: log-1\n    type: core.log\n", 1)
	want = strings.Replace(want, "  - { from: report, to: done }   # end\n",
		"  - { from: report, to: done }   # end\n  - { from: report, to: log-1 }\n  - { from: wait, to: log-1, when: failure }\n", 1)
	want += "  log-1: { x: 760, y: 120 }\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditWorkflowAddCreatesLists(t *testing.T) {
	head := "kind: Workflow\napiVersion: runnel/v1\nname: x\n"
	for _, tt := range []struct{ src, want string }{
		{head, head + "actions:\n  - id: start\n    type: core.start\n  - id: log\n    type: core.log\nlinks:\n  - { from: start, to: log }\n"},
		{head + "actions: []\nlinks: []\n", head + "actions:\n  - id: start\n    type: core.start\n  - id: log\n    type: core.log\nlinks:\n  - { from: start, to: log }\n"},
		// Without a list of links, it is created before the layout.
		{head + "actions:\n- id: start\n  type: core.start\nlayout:\n  start: { x: 0, y: 0 }\n",
			head + "actions:\n- id: start\n  type: core.start\n- id: log\n  type: core.log\nlinks:\n  - { from: start, to: log }\nlayout:\n  start: { x: 0, y: 0 }\n"},
	} {
		e := WorkflowEdit{Add: []NewAction{{ID: "start", Type: "core.start"}, {ID: "log", Type: "core.log"}}, AddLinks: []Link{{From: "start", To: "log"}}}
		if strings.Contains(tt.src, "id: start") {
			e.Add = e.Add[1:]
		}
		if got := edit(t, tt.src, e); got != tt.want {
			t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
		}
	}
}

func TestEditWorkflowLinks(t *testing.T) {
	got := edit(t, editSource, WorkflowEdit{
		DeleteLinks: []int{0},
		UpdateLinks: []LinkUpdate{{Index: 1, When: "always"}, {Index: 2, When: "failure"}},
	})
	want := strings.Replace(editSource, "  - { from: start, to: wait }\n", "", 1)
	want = strings.Replace(want, "  - from: wait\n    to: report\n", "  - from: wait\n    to: report\n    when: always\n", 1)
	want = strings.Replace(want, "  - { from: report, to: done }   # end\n", "  - { from: report, to: done, when: failure }   # end\n", 1)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	// "success" is the default and removes the condition; a loop mark stays.
	src := strings.Replace(editSource, "{ from: report, to: done }", "{ from: report, to: done, when: failure, loop: true }", 1)
	got = edit(t, src, WorkflowEdit{UpdateLinks: []LinkUpdate{{Index: 2, When: "success"}}})
	if !strings.Contains(got, "  - { from: report, to: done, loop: true }   # end\n") {
		t.Errorf("got:\n%s", got)
	}
}

func TestEditWorkflowRename(t *testing.T) {
	src := strings.Replace(editSource, "  - from: wait\n    to: report\n", "  - from: \"wait\"\n    to: report\n  - { from: wait, to: wait, loop: true }\n", 1)
	got := edit(t, src, WorkflowEdit{Rename: map[string]string{"wait": "pause"}})
	want := strings.Replace(src, "  - id: wait\n", "  - id: pause\n", 1)
	want = strings.Replace(want, "{ from: start, to: wait }", "{ from: start, to: pause }", 1)
	want = strings.Replace(want, "  - from: \"wait\"\n", "  - from: pause\n", 1)
	want = strings.Replace(want, "{ from: wait, to: wait, loop: true }", "{ from: pause, to: pause, loop: true }", 1)
	want = strings.Replace(want, "  wait:   { x: 280", "  pause:   { x: 280", 1)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
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
