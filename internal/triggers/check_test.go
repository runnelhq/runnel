package triggers

import (
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/model"
)

func project(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["project.yaml"] = "kind: Project\napiVersion: runnel/v1\nname: test\n"
	for name, content := range files {
		write(t, dir, name, content)
	}
	return dir
}

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

// An http trigger is checked against its target: placeholders are parameters that are
// not fixed, the token is set; required parameters may come with the request (S38a).
func TestCheckHTTPTrigger(t *testing.T) {
	dir := project(t, map[string]string{"workflows/target.workflow.yaml": triggerTarget})
	p, err := model.LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	http := func(path, auth, params string) *model.Trigger {
		t.Helper()
		tr, err := model.ParseTrigger("triggers/h.yaml", []byte("kind: Trigger\napiVersion: runnel/v1\nname: h\ntype: http\ntarget: workflows/target.workflow.yaml\n"+
			params+"with:\n  method: GET\n  path: "+path+"\n  auth: "+auth+"\n"))
		if err != nil {
			t.Fatal(err)
		}
		return tr
	}
	t.Setenv("RUNNEL_TEST_TOKEN", "x")
	if errs := CheckTrigger(http("/days/{DAYS}", `{ token: "${env:RUNNEL_TEST_TOKEN}" }`, "params: { MODE: full }\n"), p, dir); len(errs) != 0 {
		t.Errorf("valid: %v", errs)
	}
	if errs := CheckTrigger(http("/run", "none", ""), p, dir); len(errs) != 0 {
		t.Errorf("DAYS may come with the request: %v", errs)
	}
	for _, c := range []struct{ path, auth, params, want string }{
		{"/x/{WEEKS}", "none", "", "placeholder {WEEKS} is not a parameter of workflow workflows/target.workflow.yaml (declared: DAYS, MODE)"},
		{"/x/{MODE}", "none", "params: { MODE: full }\n", "placeholder {MODE} is also set under params"},
		{"/x", "none", "params: { MODE: half }\n", "parameter MODE (string) of workflow workflows/target.workflow.yaml"},
		{"/x", "none", "params: { YEARS: 1 }\n", "parameter YEARS is not declared"},
		{"/x", `{ token: "${env:RUNNEL_TEST_UNSET}" }`, "", "environment variable RUNNEL_TEST_UNSET with the token is not set"},
	} {
		errs := CheckTrigger(http(c.path, c.auth, c.params), p, dir)
		if len(errs) == 0 || !strings.Contains(errs.Error(), c.want) {
			t.Errorf("%s %s: got %v, want %q", c.path, c.params, errs, c.want)
		}
	}
}
