package runs

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/model"
)

const triggerTarget = `kind: Workflow
apiVersion: runnel/v1
name: target
parameters:
  - { name: DAYS, type: integer, required: true }
  - { name: MODE, enum: [full, delta], default: delta }
actions:
  - { id: start, type: core.start }
`

func trigger(t *testing.T, body string) *model.Trigger {
	t.Helper()
	tr, err := model.ParseTrigger("triggers/t.yaml", []byte("kind: Trigger\napiVersion: runnel/v1\nname: t\ntype: cron\nwith: { schedule: \"0 2 * * *\" }\n"+body))
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestCheckTrigger(t *testing.T) {
	dir := project(t, map[string]string{
		"workflows/target.workflow.yaml": triggerTarget,
		"workflows/broken.workflow.yaml": "kind: Workflow\n",
		"environments/dev.yaml":          "kind: Environment\napiVersion: runnel/v1\nname: dev\n",
	})
	p, err := model.LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if errs := CheckTrigger(trigger(t, "target: workflows/target.workflow.yaml\nenvironment: dev\nparams: { DAYS: 3, MODE: full }\n"), p, dir); len(errs) != 0 {
		t.Errorf("valid trigger: %v", errs)
	}
	for _, c := range []struct{ body, want string }{
		{"target: workflows/missing.workflow.yaml\n", "target workflows/missing.workflow.yaml not found in the project"},
		{"target: workflows/broken.workflow.yaml\n", `workflow workflows/broken.workflow.yaml has errors; check it with "runnel validate"`},
		{"target: workflows/target.workflow.yaml\n", "missing required parameter DAYS of workflow workflows/target.workflow.yaml"},
		{"target: workflows/target.workflow.yaml\nparams: { DAYS: x }\n", `parameter DAYS (integer)`},
		{"target: workflows/target.workflow.yaml\nparams: { DAYS: 1, MODE: half }\n", `parameter MODE (string)`},
		{"target: workflows/target.workflow.yaml\nparams: { DAYS: 1, WEEKS: 2 }\n", "parameter WEEKS is not declared by workflow workflows/target.workflow.yaml (declared: DAYS, MODE)"},
		{"target: workflows/target.workflow.yaml\nenvironment: prod\nparams: { DAYS: 1 }\n", `environment "prod" not found (environments: dev)`},
	} {
		errs := CheckTrigger(trigger(t, c.body), p, dir)
		if len(errs) == 0 || !strings.Contains(errs.Error(), c.want) {
			t.Errorf("%q: got %v, want %q", c.body, errs, c.want)
		}
	}
	if got := TriggerParams(trigger(t, "target: x.workflow.yaml\nparams: { DAYS: 3, MODE: full, ON: true }\n")); got["DAYS"] != "3" || got["MODE"] != "full" || got["ON"] != "true" {
		t.Errorf("params %v", got)
	}
}

// The parameter of a file trigger is declared by the target, with type string for
// mode each and json for mode batch; the trigger sets it, so params must not.
func TestCheckFileTrigger(t *testing.T) {
	dir := project(t, map[string]string{"workflows/target.workflow.yaml": `kind: Workflow
apiVersion: runnel/v1
name: target
parameters:
  - { name: FILE, required: true }
  - { name: FILES, type: json }
  - { name: KIND, enum: [a, b] }
actions:
  - { id: start, type: core.start }
`})
	p, err := model.LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	file := func(with, params string) *model.Trigger {
		t.Helper()
		tr, err := model.ParseTrigger("triggers/f.yaml", []byte("kind: Trigger\napiVersion: runnel/v1\nname: f\ntype: file\ntarget: workflows/target.workflow.yaml\n"+params+"with:\n  path: in\n"+with))
		if err != nil {
			t.Fatal(err)
		}
		return tr
	}
	if errs := CheckTrigger(file("  param: FILE\n", ""), p, dir); len(errs) != 0 {
		t.Errorf("each: %v", errs)
	}
	if errs := CheckTrigger(file("  param: FILES\n  mode: batch\n", "params: { FILE: x }\n"), p, dir); len(errs) != 0 {
		t.Errorf("batch: %v", errs)
	}
	for _, c := range []struct{ with, params, want string }{
		{"  param: NAME\n", "", "parameter NAME is not declared by workflow workflows/target.workflow.yaml (declared: FILE, FILES, KIND)"},
		{"  param: FILES\n", "params: { FILE: x }\n", "parameter FILES of workflow workflows/target.workflow.yaml has type json; with mode: each it gets the path of a file, so it must have type string"},
		{"  param: FILE\n  mode: batch\n", "", "it gets a JSON list of paths, so it must have type json"},
		{"  param: FILE\n", "params: { FILE: x }\n", "parameter FILE is set by the file trigger (with.param); remove it from params"},
		{"  param: KIND\n", "params: { FILE: x }\n", "allows only some values (enum)"},
		{"  param: FILES\n  mode: batch\n", "", "missing required parameter FILE"},
	} {
		errs := CheckTrigger(file(c.with, c.params), p, dir)
		if len(errs) == 0 || !strings.Contains(errs.Error(), c.want) {
			t.Errorf("%q %q: got %v, want %q", c.with, c.params, errs, c.want)
		}
	}
}

// settings.maxConcurrentRuns of the workflow and of the project reject further starts
// while their runs are running (S37); the queue is in the backlog.
func TestStartLimits(t *testing.T) {
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()
	limited := spec("settings: { maxConcurrentRuns: 1 }\n" + waitBody)
	limited.Params = map[string]string{"DELAY": "1h"}
	first, _, err := m.Start("workflows/a.workflow.yaml", limited)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = m.Start("workflows/a.workflow.yaml", limited)
	var limit *LimitError
	if !errors.As(err, &limit) || !strings.Contains(err.Error(), "workflows/a.workflow.yaml is already running 1 run (settings.maxConcurrentRuns: 1)") {
		t.Fatalf("second start: %v", err)
	}
	// Another file is not limited by it.
	other := spec(waitBody)
	other.Params = map[string]string{"DELAY": "1h"}
	if _, _, err := m.Start("workflows/b.workflow.yaml", other); err != nil {
		t.Fatalf("other workflow: %v", err)
	}
	// Once the run has ended, the workflow can run again.
	m.Cancel(first.ID)
	waitFor(t, m, first.ID, finished)
	if _, _, err := m.Start("workflows/a.workflow.yaml", limited); err != nil {
		t.Errorf("after the end: %v", err)
	}

	// The limit of the project counts all runs.
	if err := m.checkLimits("workflows/c.workflow.yaml", limits{project: 2}); err == nil ||
		!strings.Contains(err.Error(), "2 runs already running in the project (settings.maxConcurrentRuns in project.yaml: 2)") {
		t.Errorf("project limit: %v", err)
	}
	if err := m.checkLimits("workflows/c.workflow.yaml", limits{project: 3}); err != nil {
		t.Errorf("below the project limit: %v", err)
	}
}

func TestStartRecordsTrigger(t *testing.T) {
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()
	s := spec(waitBody)
	s.Params = map[string]string{"DELAY": "1ms"}
	s.Trigger = "cron:nightly"
	run, _, err := m.Start("workflows/a.workflow.yaml", s)
	if err != nil || run.Trigger != "cron:nightly" {
		t.Fatalf("%+v %v", run, err)
	}
}

// Drain rejects new runs and waits for the running ones without cancelling them (S41).
func TestDrain(t *testing.T) {
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()
	short := spec(waitBody)
	short.Params = map[string]string{"DELAY": "200ms"}
	run, _, err := m.Start("workflows/a.workflow.yaml", short)
	if err != nil {
		t.Fatal(err)
	}
	if m.Running() != 1 {
		t.Fatalf("running %d", m.Running())
	}
	if err := m.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d, _ := m.Get(run.ID); d.Status != "succeeded" {
		t.Errorf("drained run: %s", d.Status)
	}
	if _, _, err := m.Start("workflows/a.workflow.yaml", short); !errors.Is(err, ErrShutdown) {
		t.Errorf("start after drain: %v", err)
	}
}
