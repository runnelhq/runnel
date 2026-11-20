package runs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/model"
)

const childWorkflow = `kind: Workflow
apiVersion: runnel/v1
name: child
parameters:
  - { name: NAME, required: true }
  - { name: COUNT, type: integer, default: "1" }
  - { name: FAIL, type: boolean, default: "false" }
outputs:
  - { name: GREETING, value: "Hello {{ params.NAME }}" }
  - { name: DOUBLE, type: integer, value: "{{ params.COUNT * 2 }}" }
actions:
  - { id: start, type: core.start }
  - { id: greet, type: core.log, with: { message: "Hello {{ params.NAME }}" } }
  - { id: stop, type: core.abort, with: { message: "child gave up" } }
links:
  - { from: start, to: greet }
  - { from: greet, to: stop, when: "{{ params.FAIL }}" }
`

// project writes a project with the given workflow files (name → content) and returns its folder.
func project(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["project.yaml"] = "kind: Project\napiVersion: runnel/v1\nname: test\n"
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func parent(with string) string {
	return `kind: Workflow
apiVersion: runnel/v1
name: parent
parameters:
  - { name: TARGET, default: workflows/child.workflow.yaml }
outputs:
  - { name: RESULT, value: "{{ results.call.output.GREETING }} / {{ results.call.output.DOUBLE + 1 }}" }
actions:
  - { id: start, type: core.start }
  - id: call
    type: core.run-workflow
    with:
` + with + `
links:
  - { from: start, to: call }
`
}

type recorder struct {
	mu     sync.Mutex
	events []workflow.Event
}

func (r *recorder) observe(e workflow.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) logs() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	for _, e := range r.events {
		if e.Kind == workflow.Log {
			b.WriteString(e.Level.String() + " [" + e.ActionID + "] " + e.Message + "\n")
		}
	}
	return b.String()
}

// start prepares and runs workflows/parent.workflow.yaml of a project.
func start(t *testing.T, dir string, params map[string]string) (workflow.Result, *recorder, error) {
	t.Helper()
	return startCtx(context.Background(), t, dir, params)
}

func startCtx(ctx context.Context, t *testing.T, dir string, params map[string]string) (workflow.Result, *recorder, error) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "workflows", "parent.workflow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	p, err := Prepare(Spec{
		Name: "workflows/parent.workflow.yaml", File: filepath.Join(dir, "workflows", "parent.workflow.yaml"),
		Data: data, ProjectDir: dir, Params: params, Registry: actions.Builtin(), Observer: rec.observe,
	})
	if err != nil {
		return workflow.Result{}, rec, err
	}
	return p.Runner.Run(ctx), rec, nil
}

func TestRunWorkflowCall(t *testing.T) {
	dir := project(t, map[string]string{
		"workflows/child.workflow.yaml": childWorkflow,
		"workflows/parent.workflow.yaml": parent(`      workflow: workflows/child.workflow.yaml
      params: { NAME: Ada, COUNT: "{{ 1 + 2 }}" }`),
	})
	res, rec, err := start(t, dir, nil)
	if err != nil || res.Status != workflow.Succeeded {
		t.Fatalf("status %s: %v %v\n%s", res.Status, err, res.Err, rec.logs())
	}
	if got := res.Outputs["RESULT"]; got != "Hello Ada / 7" {
		t.Errorf("RESULT = %#v", got)
	}
	logs := rec.logs()
	for _, want := range []string{
		`DEBUG [call] workflow "child" started, run `,
		"DEBUG [call] child/greet: started (core.log)",
		"INFO [call] child/greet: Hello Ada",
		"DEBUG [call] child/stop: skipped (no incoming link applies)",
		`INFO [call] workflow "child" succeeded in `,
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("missing %q in log:\n%s", want, logs)
		}
	}
	// The called workflow has its own run id; its events are not passed on as events of the caller.
	for _, e := range rec.events {
		if e.RunID != res.RunID {
			t.Errorf("event of another run: %+v", e)
		}
	}
}

func TestRunWorkflowCallFails(t *testing.T) {
	dir := project(t, map[string]string{
		"workflows/child.workflow.yaml": childWorkflow,
		"workflows/parent.workflow.yaml": parent(`      workflow: "${TARGET}"
      params: { NAME: Ada, FAIL: "{{ params.TARGET == 'workflows/child.workflow.yaml' }}", EXTRA: "{{ params.TARGET == 'x' }}" }`),
	})
	for _, tt := range []struct {
		target, want string
	}{
		// Paths with variables are checked when the action runs.
		{"workflows/missing.workflow.yaml", "workflow file workflows/missing.workflow.yaml not found"},
		{"workflows/parent.workflow.yaml", "workflow workflows/parent.workflow.yaml calls itself (workflows/parent.workflow.yaml → workflows/parent.workflow.yaml)"},
	} {
		res, _, err := start(t, dir, map[string]string{"TARGET": tt.target})
		if err != nil || res.Status != workflow.Failed || !strings.Contains(res.Err.Error(), tt.want) {
			t.Errorf("%s: %v %v, want containing %q", tt.target, err, res.Err, tt.want)
		}
	}
	// Undeclared parameters are an error for called workflows.
	res, _, _ := start(t, dir, nil)
	if res.Err == nil || !strings.Contains(res.Err.Error(), `workflow workflows/child.workflow.yaml cannot be started: parameter(s) [EXTRA] not declared by workflow "child" (declared: NAME, COUNT, FAIL)`) {
		t.Errorf("undeclared: %v", res.Err)
	}
}

func TestRunWorkflowCallFailureOfChild(t *testing.T) {
	dir := project(t, map[string]string{
		"workflows/child.workflow.yaml": childWorkflow,
		"workflows/parent.workflow.yaml": parent(`      workflow: workflows/child.workflow.yaml
      params: { NAME: Ada, FAIL: true }`),
	})
	res, rec, _ := start(t, dir, nil)
	if res.Status != workflow.Failed || res.Err == nil || !strings.Contains(res.Err.Error(), "workflow workflows/child.workflow.yaml failed: child gave up") {
		t.Errorf("%s %v\n%s", res.Status, res.Err, rec.logs())
	}
}

func TestRunWorkflowCallCancelled(t *testing.T) {
	dir := project(t, map[string]string{
		"workflows/slow.workflow.yaml":   "kind: Workflow\napiVersion: runnel/v1\nname: slow\nactions:\n  - { id: start, type: core.start }\n  - { id: wait, type: core.wait, with: { duration: 1h } }\nlinks:\n  - { from: start, to: wait }\n",
		"workflows/parent.workflow.yaml": parent(`      workflow: workflows/slow.workflow.yaml`),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	res, _, err := startCtx(ctx, t, dir, nil)
	if err != nil || res.Status != workflow.Cancelled {
		t.Errorf("status %s: %v %v", res.Status, err, res.Err)
	}
}

func TestRunWorkflowWithoutProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "child.workflow.yaml"), []byte(childWorkflow), 0o644); err != nil {
		t.Fatal(err)
	}
	data := []byte(parent("      workflow: child.workflow.yaml\n      params: { NAME: Bo }"))
	name := filepath.Join(dir, "parent.workflow.yaml")
	p, err := Prepare(Spec{Name: name, Data: data, Registry: actions.Builtin()})
	if err != nil {
		t.Fatal(err)
	}
	if res := p.Runner.Run(context.Background()); res.Status != workflow.Succeeded || res.Outputs["RESULT"] != "Hello Bo / 3" {
		t.Errorf("%s %v %v", res.Status, res.Err, res.Outputs)
	}
}

func TestResolve(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "x.workflow.yaml")
	for _, tt := range []struct {
		project, caller, path, file, name string
	}{
		{"proj", "proj/workflows/a.workflow.yaml", "workflows/b.workflow.yaml", filepath.Join("proj", "workflows", "b.workflow.yaml"), "workflows/b.workflow.yaml"},
		{"proj", "proj/workflows/a.workflow.yaml", "./workflows/../b.workflow.yaml", filepath.Join("proj", "b.workflow.yaml"), "b.workflow.yaml"},
		{"", filepath.Join("dir", "a.workflow.yaml"), "b.workflow.yaml", filepath.Join("dir", "b.workflow.yaml"), "dir/b.workflow.yaml"},
		{"proj", "a.workflow.yaml", abs, abs, filepath.ToSlash(abs)},
	} {
		file, name := Resolve(tt.project, tt.caller, tt.path)
		if file != tt.file || name != tt.name {
			t.Errorf("Resolve(%q, %q, %q) = %q, %q; want %q, %q", tt.project, tt.caller, tt.path, file, name, tt.file, tt.name)
		}
	}
}

func TestCheckCalls(t *testing.T) {
	dir := project(t, map[string]string{
		"workflows/child.workflow.yaml":  childWorkflow,
		"workflows/a.workflow.yaml":      strings.Replace(parent("      workflow: workflows/b.workflow.yaml"), "name: parent", "name: a", 1),
		"workflows/b.workflow.yaml":      strings.Replace(parent("      workflow: workflows/a.workflow.yaml"), "name: parent", "name: b", 1),
		"workflows/broken.workflow.yaml": "kind: Workflow\nname: [",
		"workflows/parent.workflow.yaml": parent("      workflow: workflows/parent.workflow.yaml"),
	})
	check := func(with string) string {
		t.Helper()
		w, err := model.ParseWorkflow("workflows/parent.workflow.yaml", []byte(parent(with)))
		if err != nil {
			t.Fatal(err)
		}
		var msgs []string
		for _, e := range CheckCalls(w, dir, filepath.Join(dir, "workflows", "parent.workflow.yaml")) {
			msgs = append(msgs, e.Error())
		}
		return strings.Join(msgs, "\n")
	}
	for _, tt := range []struct{ with, want string }{
		{"      workflow: workflows/child.workflow.yaml\n      params: { NAME: x, COUNT: 3 }", ""},
		// Values with variables or expressions are checked when the action runs.
		{"      workflow: workflows/child.workflow.yaml\n      params: { NAME: x, COUNT: \"${N}\", FAIL: \"{{ 1 > 2 }}\" }", ""},
		{"      workflow: \"workflows/${X}.workflow.yaml\"", ""},
		{"      workflow: workflows/child.workflow.yaml", `:13:7: action "call": missing required parameter NAME of workflow workflows/child.workflow.yaml`},
		{"      workflow: workflows/child.workflow.yaml\n      params: { NAME: x, COUNT: many, COLOR: red }",
			`:14:46: action "call": parameter COLOR is not declared by workflow workflows/child.workflow.yaml (declared: NAME, COUNT, FAIL)` + "\n" +
				`workflows/parent.workflow.yaml:14:33: action "call": parameter COUNT (integer): "many" is not an integer`},
		{"      workflow: workflows/none.workflow.yaml", `:13:17: action "call": workflow file workflows/none.workflow.yaml not found`},
		{"      workflow: workflows/broken.workflow.yaml", `workflow file workflows/broken.workflow.yaml has errors; check it with "runnel validate"`},
		{"      workflow: workflows/a.workflow.yaml", ""}, // a and b call each other, but not parent
		{"      workflow: workflows/parent.workflow.yaml", `action "call": the workflow calls itself (parent → parent)`},
	} {
		got := check(tt.with)
		if tt.want == "" && got != "" || !strings.Contains(got, tt.want) {
			t.Errorf("%s:\n got %s\nwant %s", tt.with, got, tt.want)
		}
	}
	w, _ := model.ParseWorkflow("workflows/a.workflow.yaml", []byte(strings.Replace(parent("      workflow: workflows/b.workflow.yaml"), "name: parent", "name: a", 1)))
	if errs := CheckCalls(w, dir, filepath.Join(dir, "workflows", "a.workflow.yaml")); len(errs) != 1 || !strings.Contains(errs[0].Error(), "(a → b → a)") {
		t.Errorf("a → b → a: %v", errs)
	}
}
