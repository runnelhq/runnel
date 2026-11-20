package model

import (
	"strings"
	"testing"
)

const entriesSource = `kind: Workflow
apiVersion: runnel/v1
name: entries
parameters:
  # waits of the two branches
  - name: DELAY_A
    default: 2s   # short
  - { name: DELAY_B, default: 1s }   # flow style
  - name: COUNT
    type: integer
    default: "3"
outputs:
  - name: MESSAGE
    value: "{{ results.done.output }}"
actions:
  - id: start
    type: core.start
  - id: done
    type: core.log
    with:
      message: hi
links:
  - { from: start, to: done }
`

// Parameters and outputs are changed line by line: a block entry in place, a one-line
// entry ({ … }) is written anew with its comment (roadmap E7c).
func TestEditEntries(t *testing.T) {
	for _, c := range []struct {
		name string
		edit EntryEdit
		old  string
		new  string
	}{
		{"set block field", EntryEdit{List: "parameters", Op: "set", Index: 0, Field: "default", Value: "5s"},
			"    default: 2s   # short\n", "    default: 5s   # short\n"},
		{"add block field", EntryEdit{List: "parameters", Op: "set", Index: 0, Field: "description", Value: "Wait of branch A"},
			"    default: 2s   # short\n", "    default: 2s   # short\n    description: Wait of branch A\n"},
		{"remove block field", EntryEdit{List: "parameters", Op: "set", Index: 2, Field: "type", Value: nil},
			"    type: integer\n", ""},
		{"required", EntryEdit{List: "parameters", Op: "set", Index: 2, Field: "required", Value: true},
			"    default: \"3\"\n", "    default: \"3\"\n    required: true\n"},
		{"enum block", EntryEdit{List: "parameters", Op: "set", Index: 2, Field: "enum", Value: []any{"1", "3", "a, b"}},
			"    default: \"3\"\n", "    default: \"3\"\n    enum: [\"1\", \"3\", \"a, b\"]\n"},
		{"set flow field", EntryEdit{List: "parameters", Op: "set", Index: 1, Field: "default", Value: "10s"},
			"  - { name: DELAY_B, default: 1s }   # flow style\n", "  - { name: DELAY_B, default: 10s }   # flow style\n"},
		{"add flow field", EntryEdit{List: "parameters", Op: "set", Index: 1, Field: "enum", Value: []any{"1s", "2s"}},
			"  - { name: DELAY_B, default: 1s }   # flow style\n", "  - { name: DELAY_B, default: 1s, enum: [1s, 2s] }   # flow style\n"},
		{"remove flow field", EntryEdit{List: "parameters", Op: "set", Index: 1, Field: "default", Value: ""},
			"  - { name: DELAY_B, default: 1s }   # flow style\n", "  - { name: DELAY_B }   # flow style\n"},
		{"rename", EntryEdit{List: "parameters", Op: "set", Index: 0, Field: "name", Value: "WAIT_A"},
			"  - name: DELAY_A\n", "  - name: WAIT_A\n"},
		{"add parameter", EntryEdit{List: "parameters", Op: "add", Name: "PARAM_1"},
			"    default: \"3\"\noutputs:", "    default: \"3\"\n  - name: PARAM_1\noutputs:"},
		{"delete", EntryEdit{List: "parameters", Op: "delete", Index: 1},
			"  - { name: DELAY_B, default: 1s }   # flow style\n", ""},
		{"move up", EntryEdit{List: "parameters", Op: "move", Index: 1, By: -1},
			"  - name: DELAY_A\n    default: 2s   # short\n  - { name: DELAY_B, default: 1s }   # flow style\n",
			"  - { name: DELAY_B, default: 1s }   # flow style\n  - name: DELAY_A\n    default: 2s   # short\n"},
		{"output value", EntryEdit{List: "outputs", Op: "set", Index: 0, Field: "value", Value: "{{ results.done.status }}"},
			"    value: \"{{ results.done.output }}\"\n", "    value: \"{{ results.done.status }}\"\n"},
		{"add output", EntryEdit{List: "outputs", Op: "add", Name: "STATUS"},
			"    value: \"{{ results.done.output }}\"\n", "    value: \"{{ results.done.output }}\"\n  - name: STATUS\n    value: \"\"\n"},
		{"delete last output", EntryEdit{List: "outputs", Op: "delete", Index: 0},
			"outputs:\n  - name: MESSAGE\n    value: \"{{ results.done.output }}\"\n", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(entriesSource, c.old) {
				t.Fatalf("test source has no %q", c.old)
			}
			want := strings.Replace(entriesSource, c.old, c.new, 1)
			got := edit(t, entriesSource, WorkflowEdit{Entries: []EntryEdit{c.edit}})
			if got != want {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
			// Field problems (e.g. an allowed value of the wrong type) are reported by
			// /validate; the file must stay readable.
			if w, err := ReadWorkflow("x", []byte(got)); w == nil {
				t.Errorf("result cannot be read: %v", err)
			}

		})
	}
}

// A workflow without parameters gets the list before its actions.
func TestEditEntriesCreatesList(t *testing.T) {
	src := "kind: Workflow\napiVersion: runnel/v1\nname: x\nactions:\n  - id: start\n    type: core.start\n"
	got := edit(t, src, WorkflowEdit{Entries: []EntryEdit{{List: "parameters", Op: "add", Name: "FILE"}}})
	want := "kind: Workflow\napiVersion: runnel/v1\nname: x\nparameters:\n  - name: FILE\nactions:\n  - id: start\n    type: core.start\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	// One-line entries get one more of them.
	src = strings.Replace(src, "name: x\n", "name: x\nparameters:\n  - { name: A, default: x }\n", 1)
	got = edit(t, src, WorkflowEdit{Entries: []EntryEdit{{List: "parameters", Op: "add", Name: "B"}}})
	if !strings.Contains(got, "  - { name: A, default: x }\n  - { name: B }\n") {
		t.Errorf("got:\n%s", got)
	}
}

func TestEditEntriesErrors(t *testing.T) {
	for _, c := range []struct {
		edit EntryEdit
		want string
	}{
		{EntryEdit{List: "inputs", Op: "add", Name: "X"}, `no list "inputs"`},
		{EntryEdit{List: "parameters", Op: "set", Index: 9, Field: "default", Value: "x"}, "parameter #10: it does not exist"},
		{EntryEdit{List: "parameters", Op: "set", Index: 0, Field: "value", Value: "x"}, `field "value" of a parameter`},
		{EntryEdit{List: "outputs", Op: "set", Index: 0, Field: "value", Value: nil}, `"value" is required`},
		{EntryEdit{List: "parameters", Op: "move", Index: 0, By: -1}, "cannot move parameter #1"},
		{EntryEdit{List: "parameters", Op: "add", Name: " "}, "the name is empty"},
		{EntryEdit{List: "parameters", Op: "set", Index: 0, Field: "enum", Value: "x"}, "expected a list of texts"},
		{EntryEdit{List: "parameters", Op: "rename", Index: 0}, `unknown change "rename"`},
	} {
		_, err := EditWorkflow("x.workflow.yaml", []byte(entriesSource), WorkflowEdit{Entries: []EntryEdit{c.edit}})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: got %v, want %q", c.edit, err, c.want)
		}
	}
	src := strings.Replace(entriesSource, "outputs:\n  - name: MESSAGE\n    value: \"{{ results.done.output }}\"\n", "outputs: [{ name: M, value: x }]\n", 1)
	_, err := EditWorkflow("x.workflow.yaml", []byte(src), WorkflowEdit{Entries: []EntryEdit{{List: "outputs", Op: "add", Name: "N"}}})
	if err == nil || !strings.Contains(err.Error(), "flow style") {
		t.Errorf("flow list: %v", err)
	}
}

// forEach, parallel, stopOnError and a mapping (with.params) can be set on an action.
func TestEditWorkflowSetRepeatAndMap(t *testing.T) {
	src := "kind: Workflow\napiVersion: runnel/v1\nname: x\nactions:\n  - id: start\n    type: core.start\n  - id: call\n    type: core.run-workflow\n    with:\n      workflow: workflows/child.workflow.yaml\nlinks:\n  - { from: start, to: call }\n"
	got := edit(t, src, WorkflowEdit{Set: []SetField{{ID: "call", Field: "forEach", Value: "{{ [1, 2] }}"}}})
	if !strings.Contains(got, "    forEach: \"{{ [1, 2] }}\"\n") {
		t.Errorf("forEach:\n%s", got)
	}
	got = edit(t, got, WorkflowEdit{Set: []SetField{{ID: "call", Field: "parallel", Value: 2}}})
	got = edit(t, got, WorkflowEdit{Set: []SetField{{ID: "call", Field: "stopOnError", Value: false}}})
	if !strings.Contains(got, "    parallel: 2\n    stopOnError: false\n") {
		t.Errorf("parallel, stopOnError:\n%s", got)
	}
	m := []KeyValue{{Key: "FILE", Value: "{{ row }}"}, {Key: "COUNT", Value: "5"}}
	got = edit(t, got, WorkflowEdit{Set: []SetField{{ID: "call", Field: "with.params", Map: m}}})
	if !strings.Contains(got, "      workflow: workflows/child.workflow.yaml\n      params:\n        FILE: \"{{ row }}\"\n        COUNT: \"5\"\n") {
		t.Errorf("params:\n%s", got)
	}
	// Keys already in the file keep their order (the editor sends them sorted).
	ordered := edit(t, got, WorkflowEdit{Set: []SetField{{ID: "call", Field: "with.params", Map: []KeyValue{{Key: "COUNT", Value: "6"}, {Key: "FILE", Value: "{{ row }}"}, {Key: "A", Value: "x"}}}}})
	if !strings.Contains(ordered, "      params:\n        FILE: \"{{ row }}\"\n        COUNT: \"6\"\n        A: x\n") {
		t.Errorf("order:\n%s", ordered)
	}
	// Setting it again replaces the whole mapping; nil removes it.
	got = edit(t, got, WorkflowEdit{Set: []SetField{{ID: "call", Field: "with.params", Map: m[1:]}}})
	if !strings.Contains(got, "      params:\n        COUNT: \"5\"\n    forEach") {
		t.Errorf("params again:\n%s", got)
	}
	got = edit(t, got, WorkflowEdit{Set: []SetField{{ID: "call", Field: "with.params"}}})
	if strings.Contains(got, "params") {
		t.Errorf("params removed:\n%s", got)
	}
	if _, err := ParseWorkflow("x", []byte(got)); err != nil {
		t.Errorf("result does not parse: %v", err)
	}
	_, err := EditWorkflow("x.workflow.yaml", []byte(src), WorkflowEdit{Set: []SetField{{ID: "call", Field: "with.params", Map: []KeyValue{{Key: "a b", Value: "x"}}}}})
	if err == nil || !strings.Contains(err.Error(), `invalid or duplicate key "a b"`) {
		t.Errorf("invalid key: %v", err)
	}
}

// Pipelines have parameters, but no outputs.
func TestEditPipelineEntries(t *testing.T) {
	src := "kind: Pipeline\napiVersion: runnel/v1\nname: p\ntransforms:\n  - id: gen\n    type: core.generate-rows\n"
	out, err := EditPipeline("x.pipeline.yaml", []byte(src), PipelineEdit{Entries: []EntryEdit{{List: "parameters", Op: "add", Name: "ROWS"}}})
	if err != nil || !strings.Contains(string(out), "name: p\nparameters:\n  - name: ROWS\ntransforms:") {
		t.Errorf("%v\n%s", err, out)
	}
	_, err = EditPipeline("x.pipeline.yaml", []byte(src), PipelineEdit{Entries: []EntryEdit{{List: "outputs", Op: "add", Name: "X"}}})
	if err == nil || !strings.Contains(err.Error(), `the pipeline has no list "outputs"`) {
		t.Errorf("outputs: %v", err)
	}
}
