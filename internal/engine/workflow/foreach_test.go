package workflow

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/model"
)

func TestForEach(t *testing.T) {
	w := parse(t, `
outputs:
  - { name: SECOND, type: integer, value: "{{ results.each.output.items[1].output }}" }
  - { name: ROW, type: integer, value: "{{ results.each.output.items[2].row }}" }
  - { name: COUNT, type: integer, value: "{{ results.each.output.succeeded }}" }
actions:
  - { id: start, type: core.start }
  - id: each
    type: test.out
    forEach: "{{ [1, 2, 3] }}"
    with: { value: "{{ row * 10 }}" }
links:
  - { from: start, to: each }
`)
	res, _ := runWorkflow(t, w, nil)
	if res.Status != Succeeded || fmt.Sprint(res.Outputs) != "map[COUNT:3 ROW:3 SECOND:20]" {
		t.Errorf("%s %v %v", res.Status, res.Err, res.Outputs)
	}
}

func TestForEachStopsAtTheFirstError(t *testing.T) {
	body := `
outputs:
  - { name: SUMMARY, value: "{{ results.each.output.succeeded }}/{{ results.each.output.failed }}/{{ len(results.each.output.items) }}" }
actions:
  - { id: start, type: core.start }
  - id: each
    type: test.failif
    forEach: "{{ [1, 2, 3, 4] }}"
    with: { value: "{{ row }}", fail: "{{ row == 2 }}" }
  - { id: report, type: test.ok, with: { value: "{{ results.each.output.succeeded }}/{{ results.each.output.failed }}/{{ len(results.each.output.items) }}" } }
links:
  - { from: start, to: each }
  - { from: each, to: report, when: always }
`
	// stopOnError (default): entry 2 fails the action; 3 and 4 do not run.
	res, trace := runWorkflow(t, parse(t, body), nil)
	logs := strings.Join(trace, "\n")
	if res.Status != Succeeded || !strings.Contains(logs, "finished each: failed") || !strings.Contains(logs, "log report: 1/1/2") {
		t.Errorf("stop: %s\n%s", res.Status, logs)
	}

	// stopOnError: false: all entries run, a warning per failed one, the action fails at the end.
	body = strings.Replace(body, "    forEach:", "    stopOnError: false\n    forEach:", 1)
	r, err := Prepare(parse(t, body), Options{Registry: testRegistry(t), Observer: func(e Event) {
		if e.Kind == ActionFinished && e.ActionID == "each" {
			if e.Err == nil || e.Err.Error() != "1 of 4 entries failed; first: entry 2: entry 2 failed" {
				t.Errorf("error %v", e.Err)
			}
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	r.Run(context.Background())
	_, trace = runWorkflow(t, parse(t, body), nil)
	logs = strings.Join(trace, "\n")
	if !strings.Contains(logs, "log each: entry 2 of 4 failed: entry 2 failed") || !strings.Contains(logs, "log report: 3/1/4") {
		t.Errorf("continue:\n%s", logs)
	}
}

func TestForEachStopErrorMessage(t *testing.T) {
	w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: each, type: test.failif, forEach: "{{ ['a', 'b'] }}", with: { value: "{{ row }}", fail: true } }
links:
  - { from: start, to: each }
`)
	res, _ := runWorkflow(t, w, nil)
	if res.Status != Failed || !strings.Contains(res.Err.Error(), "entry 1 of 2: entry a failed") {
		t.Errorf("%s %v", res.Status, res.Err)
	}
}

func TestForEachParallel(t *testing.T) {
	w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: each, type: core.wait, forEach: "{{ [1, 2, 3, 4] }}", parallel: 4, with: { duration: 300ms } }
links:
  - { from: start, to: each }
`)
	start := time.Now()
	res, _ := runWorkflow(t, w, nil)
	if d := time.Since(start); res.Status != Succeeded || d > 900*time.Millisecond {
		t.Errorf("%s %v after %s; 4 entries of 300 ms with parallel 4 should take about 300 ms", res.Status, res.Err, d)
	}
}

func TestForEachValues(t *testing.T) {
	for _, tt := range []struct{ list, want string }{
		{"{{ [] }}", ""},
		{"{{ 'text' }}", "forEach: the expression must return a list, got text"},
		{"{{ nil }}", ""},
	} {
		w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: each, type: test.ok, forEach: "`+tt.list+`", with: { value: x } }
links:
  - { from: start, to: each }
`)
		res, _ := runWorkflow(t, w, nil)
		switch {
		case tt.want == "" && res.Status != Succeeded:
			t.Errorf("%s: %s %v", tt.list, res.Status, res.Err)
		case tt.want != "" && (res.Err == nil || !strings.Contains(res.Err.Error(), tt.want)):
			t.Errorf("%s: %v, want %q", tt.list, res.Err, tt.want)
		}
	}
}

func TestForEachValidation(t *testing.T) {
	for _, tt := range []struct{ action, want string }{
		{`{ id: a, type: test.ok, with: { value: "{{ row.x }}" } }`, "unknown name row"},
		{`{ id: a, type: test.ok, parallel: 2 }`, "parallel is only possible with forEach"},
		{`{ id: a, type: test.ok, stopOnError: false }`, "stopOnError is only possible with forEach"},
		{`{ id: a, type: test.ok, forEach: "${LIST}" }`, "forEach must be an expression that returns a list"},
		{`{ id: a, type: test.ok, forEach: "{{ rows }}" }`, "unknown name rows"},
		{`{ id: a, type: test.ok, forEach: "{{ [1] }}", parallel: 100 }`, "parallel must be between 1 and 64"},
	} {
		w, err := model.ParseWorkflow("t.workflow.yaml", []byte("kind: Workflow\napiVersion: runnel/v1\nname: t\nactions:\n  - "+tt.action+"\n"))
		if err == nil {
			err = w.Validate().Err()
		}
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v, want %q", tt.action, err, tt.want)
		}
	}
}
