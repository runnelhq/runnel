package workflow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions/core"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/vars"
)

// testRegistry contains the core actions plus:
//
//	test.ok    succeeds and logs its "value"
//	test.fail  fails with "failed: <value>"
//	test.flaky fails the first <n> times, then succeeds
//	test.out   succeeds with its "value" as output (result.output)
func testRegistry(t *testing.T) *action.Registry {
	t.Helper()
	r := action.NewRegistry()
	core.Register(r)
	r.Register(action.Definition{Type: "test.ok", New: func(with map[string]any) (action.Action, error) {
		return testAction{value: fmt.Sprint(with["value"])}, nil
	}})
	r.Register(action.Definition{Type: "test.fail", New: func(with map[string]any) (action.Action, error) {
		return testAction{value: fmt.Sprint(with["value"]), fail: true}, nil
	}})
	r.Register(action.Definition{Type: "test.failif", New: func(with map[string]any) (action.Action, error) {
		return action.Func(func(context.Context, *action.ExecContext) (action.Result, error) {
			if with["fail"] == true {
				return action.Result{}, fmt.Errorf("entry %v failed", with["value"])
			}
			return action.Result{Output: with["value"]}, nil
		}), nil
	}})
	r.Register(action.Definition{Type: "test.out", New: func(with map[string]any) (action.Action, error) {
		return action.Func(func(context.Context, *action.ExecContext) (action.Result, error) {
			return action.Result{Output: with["value"]}, nil
		}), nil
	}})
	var mu sync.Mutex
	calls := map[string]int{}
	r.Register(action.Definition{Type: "test.flaky", New: func(map[string]any) (action.Action, error) {
		return action.Func(func(_ context.Context, ec *action.ExecContext) (action.Result, error) {
			mu.Lock()
			defer mu.Unlock()
			calls[ec.ActionID]++
			if calls[ec.ActionID] <= 2 {
				return action.Result{}, fmt.Errorf("attempt %d failed", calls[ec.ActionID])
			}
			return action.Result{}, nil
		}), nil
	}})
	return r
}

type testAction struct {
	value string
	fail  bool
}

func (a testAction) Execute(_ context.Context, ec *action.ExecContext) (action.Result, error) {
	ec.Log(slog.LevelInfo, a.value)
	if a.fail {
		return action.Result{}, errors.New("failed: " + a.value)
	}
	return action.Result{}, nil
}

func parse(t *testing.T, yaml string) *model.Workflow {
	t.Helper()
	w, err := model.ParseWorkflow("test.workflow.yaml", []byte("kind: Workflow\napiVersion: runnel/v1\nname: test\n"+yaml))
	if err != nil {
		t.Fatal(err)
	}
	if diags := w.Validate(); diags.HasErrors() {
		t.Fatal(diags)
	}
	return w
}

// runWorkflow runs a workflow and returns the result and a trace of the events,
// one line per event: "started a", "log a: text", "finished a: failed".
func runWorkflow(t *testing.T, w *model.Workflow, scope *vars.Scope) (Result, []string) {
	t.Helper()
	var trace []string
	r, err := Prepare(w, Options{Registry: testRegistry(t), Scope: scope, Observer: func(e Event) {
		switch e.Kind {
		case ActionStarted:
			trace = append(trace, "started "+e.ActionID)
		case ActionFinished:
			trace = append(trace, fmt.Sprintf("finished %s: %s", e.ActionID, e.Status))
		case Log:
			trace = append(trace, fmt.Sprintf("log %s: %s", e.ActionID, e.Message))
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	return r.Run(context.Background()), trace
}

func TestRunSequence(t *testing.T) {
	w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: a, type: test.ok, with: { value: one } }
  - { id: b, type: test.ok, with: { value: two } }
links:
  - { from: start, to: a }
  - { from: a, to: b }
`)
	res, trace := runWorkflow(t, w, nil)
	if res.Status != Succeeded || res.Err != nil {
		t.Fatalf("result = %+v", res)
	}
	want := "started start|finished start: succeeded|started a|log a: one|finished a: succeeded|started b|log b: two|finished b: succeeded"
	if got := strings.Join(trace, "|"); got != want {
		t.Errorf("trace:\n%s\nwant:\n%s", got, want)
	}
	if len(res.RunID) != 26 {
		t.Errorf("run id %q is not a ULID", res.RunID)
	}
}

func TestRunLinkConditions(t *testing.T) {
	tests := []struct {
		name       string
		yaml       string
		wantStatus Status
		wantErr    string
		wantTrace  string
	}{
		{
			name: "failure link handles the error",
			yaml: `
actions:
  - { id: start, type: core.start }
  - { id: a, type: test.fail, with: { value: x } }
  - { id: ok, type: test.ok, with: { value: ok } }
  - { id: handle, type: test.ok, with: { value: handled } }
links:
  - { from: start, to: a }
  - { from: a, to: ok }
  - { from: a, to: handle, when: failure }
`,
			wantStatus: Succeeded,
			wantTrace:  "start,a,handle",
		},
		{
			name: "unhandled failure fails the run",
			yaml: `
actions:
  - { id: start, type: core.start }
  - { id: a, type: test.fail, with: { value: boom } }
  - { id: b, type: test.ok }
links:
  - { from: start, to: a }
  - { from: a, to: b }
`,
			wantStatus: Failed,
			wantErr:    `action "a" failed: failed: boom`,
			wantTrace:  "start,a",
		},
		{
			name: "always link after failure; run fails if the last action failed",
			yaml: `
actions:
  - { id: start, type: core.start }
  - { id: a, type: test.ok }
  - { id: b, type: test.fail, with: { value: late } }
links:
  - { from: start, to: a }
  - { from: a, to: b, when: always }
`,
			wantStatus: Failed,
			wantErr:    "failed: late",
			wantTrace:  "start,a,b",
		},
		{
			name: "continueOnError treats the failure as success",
			yaml: `
actions:
  - { id: start, type: core.start }
  - { id: a, type: test.fail, continueOnError: true }
  - { id: b, type: test.ok }
  - { id: c, type: test.ok }
links:
  - { from: start, to: a }
  - { from: a, to: b }
  - { from: a, to: c, when: failure }
`,
			wantStatus: Succeeded,
			wantTrace:  "start,a,b",
		},
		{
			name: "core.success ends the run",
			yaml: `
actions:
  - { id: start, type: core.start }
  - { id: done, type: core.success, with: { message: all good } }
links:
  - { from: start, to: done }
`,
			wantStatus: Succeeded,
			wantTrace:  "start,done",
		},
		{
			name: "core.abort fails the run without following failure links",
			yaml: `
actions:
  - { id: start, type: core.start }
  - { id: stop, type: core.abort, with: { message: "input missing" } }
  - { id: never, type: test.ok }
links:
  - { from: start, to: stop }
  - { from: stop, to: never, when: always }
`,
			wantStatus: Failed,
			wantErr:    "input missing",
			wantTrace:  "start,stop",
		},
		{
			name: "loop link repeats until success",
			yaml: `
actions:
  - { id: start, type: core.start }
  - { id: try, type: test.flaky }
  - { id: pause, type: test.ok }
  - { id: done, type: test.ok }
links:
  - { from: start, to: try }
  - { from: try, to: done }
  - { from: try, to: pause, when: failure }
  - { from: pause, to: try, loop: true }
`,
			wantStatus: Succeeded,
			wantTrace:  "start,try,pause,try,pause,try,done",
		},
		{
			name: "two links apply: both branches run",
			yaml: `
actions:
  - { id: start, type: core.start }
  - { id: a, type: test.ok }
  - { id: b, type: test.ok }
links:
  - { from: start, to: a }
  - { from: start, to: b }
`,
			wantStatus: Succeeded,
			wantTrace:  "start,a,b",
		},
		{
			name: "disabled links are ignored",
			yaml: `
actions:
  - { id: start, type: core.start }
  - { id: a, type: test.ok }
  - { id: b, type: test.ok }
links:
  - { from: start, to: a }
  - { from: start, to: b, disabled: true }
`,
			wantStatus: Succeeded,
			wantTrace:  "start,a",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, trace := runWorkflow(t, parse(t, tt.yaml), nil)
			if res.Status != tt.wantStatus {
				t.Errorf("status = %s, want %s (err: %v)", res.Status, tt.wantStatus, res.Err)
			}
			if tt.wantErr == "" && res.Err != nil {
				t.Errorf("unexpected error: %v", res.Err)
			}
			if tt.wantErr != "" && (res.Err == nil || !strings.Contains(res.Err.Error(), tt.wantErr)) {
				t.Errorf("error = %v, want containing %q", res.Err, tt.wantErr)
			}
			var started []string
			for _, line := range trace {
				if id, ok := strings.CutPrefix(line, "started "); ok {
					started = append(started, id)
				}
			}
			if got := strings.Join(started, ","); got != tt.wantTrace {
				t.Errorf("executed %s, want %s", got, tt.wantTrace)
			}
		})
	}
}

func TestRunEndlessLoopStops(t *testing.T) {
	w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: a, type: test.ok }
  - { id: b, type: test.ok }
links:
  - { from: start, to: a }
  - { from: a, to: b }
  - { from: b, to: a, loop: true }
`)
	r, err := Prepare(w, Options{Registry: testRegistry(t), MaxActionRuns: 5})
	if err != nil {
		t.Fatal(err)
	}
	res := r.Run(context.Background())
	if res.Status != Failed || !strings.Contains(res.Err.Error(), `action "a" ran more than 5 times`) {
		t.Errorf("result = %+v", res)
	}
}

func TestRunVariables(t *testing.T) {
	w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: greet, type: test.ok, with: { value: "Hello ${TARGET} ($${literal})" } }
  - { id: missing, type: test.ok, with: { value: "${NOPE}" } }
  - { id: handled, type: test.ok, with: { value: handled } }
links:
  - { from: start, to: greet }
  - { from: greet, to: missing }
  - { from: missing, to: handled, when: failure }
`)
	scope := vars.NewScope(vars.Layer{Name: "test", Values: map[string]string{"TARGET": "World"}})
	res, trace := runWorkflow(t, w, scope)
	if res.Status != Succeeded {
		t.Fatalf("result = %+v", res)
	}
	got := strings.Join(trace, "\n")
	if !strings.Contains(got, "log greet: Hello World (${literal})") {
		t.Errorf("variable not expanded:\n%s", got)
	}
	if !strings.Contains(got, "finished missing: failed") || !strings.Contains(got, "log handled: handled") {
		t.Errorf("undefined variable should fail the action:\n%s", got)
	}
}

func TestRunCancelled(t *testing.T) {
	w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: wait, type: core.wait, with: { duration: 1h } }
links:
  - { from: start, to: wait }
`)
	r, err := Prepare(w, Options{Registry: testRegistry(t)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	res := r.Run(ctx)
	if res.Status != Cancelled || !errors.Is(res.Err, context.DeadlineExceeded) {
		t.Errorf("result = %+v", res)
	}
}

func TestPrepareErrors(t *testing.T) {
	w := parse(t, `
settings:
  timeout: 1h
actions:
  - { id: start, type: core.start }
  - { id: a, type: file.copy }
  - { id: b, type: core.log, with: { mesage: typo } }
  - { id: c, type: test.ok, timeout: 5m, retry: { attempts: 3 } }
  - { id: d, type: test.ok }
links:
  - { from: start, to: a }
  - { from: a, to: b, when: "{{ result.ok }}" }
  - { from: a, to: c, when: failure }
  - { from: b, to: d }
  - { from: c, to: d }
`)
	_, err := Prepare(w, Options{Registry: testRegistry(t)})
	if _, ok := errors.AsType[model.Errors](err); !ok {
		t.Fatalf("want model.Errors, got %v", err)
	}
	for _, want := range []string{
		// The list of available types grows with every new action; only check that it is shown.
		`9:20: action "a": unknown action type "file.copy" (available: [core.abort `,
		`10:38: action "b" (core.log): unknown field "mesage"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}

func TestNewRunID(t *testing.T) {
	a := NewRunID(time.UnixMilli(1_700_000_000_000))
	b := NewRunID(time.UnixMilli(1_700_000_000_001))
	if len(a) != 26 || len(b) != 26 {
		t.Fatalf("length: %q %q", a, b)
	}
	if a[:10] >= b[:10] {
		t.Errorf("ids are not sortable by time: %q >= %q", a, b)
	}
	if a[:10] != "01HF7YAT00" {
		t.Errorf("time part = %q, want 01HF7YAT00", a[:10])
	}
	if first, second := NewRunID(time.UnixMilli(0)), NewRunID(time.UnixMilli(0)); first == second {
		t.Error("random part is not random")
	}
}

func TestRunOutputs(t *testing.T) {
	w := parse(t, `
outputs:
  - { name: GREETING, value: "Hello ${TARGET}" }
  - { name: COUNT, type: integer, value: "${N}" }
  - { name: DATA, type: json, value: '{"ok": true}' }
actions:
  - { id: start, type: core.start }
`)
	scope := vars.NewScope(vars.Layer{Name: "test", Values: map[string]string{"TARGET": "World", "N": "42"}})
	var finished Event
	r, err := Prepare(w, Options{Registry: testRegistry(t), Scope: scope, Observer: func(e Event) {
		if e.Kind == RunFinished {
			finished = e
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	res := r.Run(context.Background())
	want := map[string]any{"GREETING": "Hello World", "COUNT": int64(42), "DATA": map[string]any{"ok": true}}
	if res.Status != Succeeded || fmt.Sprint(res.Outputs) != fmt.Sprint(want) {
		t.Errorf("result = %+v, want outputs %v", res, want)
	}
	if fmt.Sprint(finished.Outputs) != fmt.Sprint(want) {
		t.Errorf("RunFinished outputs = %v", finished.Outputs)
	}

	// A value that does not match the declared type fails the run.
	scope = vars.NewScope(vars.Layer{Name: "test", Values: map[string]string{"TARGET": "World", "N": "many"}})
	r, _ = Prepare(w, Options{Registry: testRegistry(t), Scope: scope})
	if res := r.Run(context.Background()); res.Status != Failed || res.Outputs != nil ||
		!strings.Contains(res.Err.Error(), `output "COUNT": "many" is not an integer`) {
		t.Errorf("type mismatch: %+v", res)
	}
}

func TestRunOutputsNotEvaluatedOnFailure(t *testing.T) {
	w := parse(t, `
outputs:
  - { name: X, value: "${UNDEFINED}" }
actions:
  - { id: start, type: core.start }
  - { id: stop, type: core.abort }
links:
  - { from: start, to: stop }
`)
	res, _ := runWorkflow(t, w, nil)
	if res.Status != Failed || res.Outputs != nil || strings.Contains(res.Err.Error(), "UNDEFINED") {
		t.Errorf("result = %+v", res)
	}
}

func TestExpressions(t *testing.T) {
	w := parse(t, `
parameters:
  - { name: LIMIT, type: integer, default: "{{ 1 + 1 }}" }
  - { name: DAY, default: "{{ formatDate(addDays(parseDate('2026-11-19', 'yyyy-MM-dd'), 1), 'dd.MM.yyyy') }}" }
outputs:
  - { name: FILES, type: integer, value: "{{ len(results.list.output) }}" }
  - { name: SUMMARY, value: "{{ results.list.status }}: {{ results.list.output }}" }
actions:
  - { id: start, type: core.start }
  - { id: list, type: test.out, with: { value: [a.csv, b.csv, c.csv] } }
  - { id: many, type: test.ok, with: { value: "{{ len(result.output) }} files from {{ result.id }} on {{ params.DAY }}" } }
  - { id: few, type: test.ok, with: { value: few } }
  - { id: typed, type: test.ok, with: { value: "{{ params.LIMIT * 10 }}" } }
links:
  - { from: start, to: list }
  - { from: list, to: many, when: "{{ len(result.output) > params.LIMIT }}" }
  - { from: list, to: few, when: "{{ len(result.output) <= params.LIMIT }}" }
  - { from: many, to: typed, when: "{{ results.list.success && run.workflow == 'test' }}" }
`)
	res, trace := runWorkflow(t, w, nil)
	if res.Status != Succeeded {
		t.Fatalf("status %s: %v\n%s", res.Status, res.Err, strings.Join(trace, "\n"))
	}
	got := strings.Join(trace, "\n")
	for _, want := range []string{"log many: 3 files from list on 20.11.2026", "log typed: 20"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "started few") {
		t.Errorf("few must not run:\n%s", got)
	}
	if res.Outputs["FILES"] != int64(3) || res.Outputs["SUMMARY"] != `succeeded: ["a.csv","b.csv","c.csv"]` {
		t.Errorf("outputs %#v", res.Outputs)
	}
}

func TestExpressionsWithVariablesAndFailures(t *testing.T) {
	w := parse(t, `
parameters:
  - { name: NAME, default: World }
actions:
  - { id: start, type: core.start }
  - { id: bad, type: test.fail, with: { value: "${NAME}" } }
  - { id: report, type: test.ok, with: { value: "{{ result.id }} failed: {{ result.error }} ({{ run.lastError }})" } }
links:
  - { from: start, to: bad }
  - { from: bad, to: report, when: "{{ !result.success && '${NAME}' == 'World' }}" }
`)
	res, trace := runWorkflow(t, w, vars.NewScope(vars.Layer{Name: model.ParameterDefaultsLayer, Values: map[string]string{"NAME": "World"}}))
	got := strings.Join(trace, "\n")
	if res.Status != Succeeded || !strings.Contains(got, "log report: bad failed: failed: World (failed: World)") {
		t.Errorf("status %s: %v\n%s", res.Status, res.Err, got)
	}
}

func TestExpressionErrorsFailTheRun(t *testing.T) {
	for _, tt := range []struct{ name, yaml, want string }{
		{"condition not boolean", `
actions:
  - { id: start, type: core.start }
  - { id: a, type: test.ok }
links:
  - { from: start, to: a, when: "{{ result.id }}" }
`, `link #1 (start → a): expression {{ result.id }}: the result must be true or false, got start`},
		{"error in configuration", `
actions:
  - { id: start, type: core.start }
  - { id: a, type: test.ok, with: { value: "{{ result.output.x.y }}" } }
links:
  - { from: start, to: a }
`, `action "a" failed: value: expression {{ result.output.x.y }}`},
		{"error in parameter default", `
parameters:
  - { name: D, default: "{{ parseDate('x', 'yyyy') }}" }
actions:
  - { id: start, type: core.start }
`, `parameter "D": default value: expression {{ parseDate('x', 'yyyy') }}`},
		{"error in output", `
outputs:
  - { name: N, type: integer, value: "{{ 'x' }}" }
actions:
  - { id: start, type: core.start }
`, `output "N"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, _ := runWorkflow(t, parse(t, tt.yaml), nil)
			if res.Status != Failed || res.Err == nil || !strings.Contains(res.Err.Error(), tt.want) {
				t.Errorf("status %s, err %v, want containing %q", res.Status, res.Err, tt.want)
			}
		})
	}
}

// A workflow with core.success that ends without reaching it still succeeds, with a warning.
func TestRunWarnsWhenEndIsNotReached(t *testing.T) {
	for _, tt := range []struct {
		name, links, want string
	}{
		{"reached", "  - { from: a, to: done }\n", ""},
		{"disabled link", "  - { from: a, to: done, disabled: true }\n",
			`log : workflow ended without reaching its end action "done" (no link led there)`},
		{"condition not met", "  - { from: a, to: done, when: failure }\n",
			`log : workflow ended without reaching its end action "done" (no link led there)`},
	} {
		w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: a, type: test.ok, with: { value: one } }
  - { id: done, type: core.success }
links:
  - { from: start, to: a }
`+tt.links)
		res, trace := runWorkflow(t, w, nil)
		if res.Status != Succeeded {
			t.Errorf("%s: status %s, %v", tt.name, res.Status, res.Err)
		}
		var warnings []string
		for _, l := range trace {
			if strings.Contains(l, "end action") {
				warnings = append(warnings, l)
			}
		}
		if got := strings.Join(warnings, "|"); got != tt.want {
			t.Errorf("%s: warnings %q, want %q", tt.name, got, tt.want)
		}
	}

	// Without core.success (only an abort on the failure path), ending without it is normal.
	w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: a, type: test.ok, with: { value: one } }
  - { id: stop, type: core.abort }
links:
  - { from: start, to: a }
  - { from: a, to: stop, when: failure }
`)
	if res, trace := runWorkflow(t, w, nil); res.Status != Succeeded || strings.Contains(strings.Join(trace, "|"), "end action") {
		t.Errorf("abort only: %s %v", res.Status, trace)
	}
	// Two unreached end actions are named together.
	w = parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: a, type: test.ok, with: { value: one } }
  - { id: ok1, type: core.success }
  - { id: ok2, type: core.success }
links:
  - { from: start, to: a }
  - { from: a, to: ok1, disabled: true }
  - { from: a, to: ok2, disabled: true }
`)
	if _, trace := runWorkflow(t, w, nil); !strings.Contains(strings.Join(trace, "|"), `without reaching any of its end actions "ok1", "ok2"`) {
		t.Errorf("two: %v", trace)
	}
}

// TestNumberInTextField: a number in a field of type text (message: 42) is accepted
// before the run and passed on as text, like in pipelines (e.g. status: 404 of http.respond).
func TestNumberInTextField(t *testing.T) {
	w := parse(t, `actions:
  - { id: start, type: core.start }
  - { id: say, type: core.log, with: { message: 42 } }
links:
  - { from: start, to: say }
`)
	res, trace := runWorkflow(t, w, nil)
	if res.Status != Succeeded || !slices.Contains(trace, "log say: 42") {
		t.Errorf("%s %v: %v", res.Status, res.Err, trace)
	}
}
