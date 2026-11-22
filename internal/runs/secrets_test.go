package runs

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/model"
)

// loginAction stands for an action with a password field (as later db.* or mail.send);
// it writes the password to its log and output, which the mask must hide.
type loginAction struct {
	User     string `json:"user" doc:"User name"`
	Password string `json:"password" doc:"Password of the user" schema:"format=runnel-secret"`
}

func (a *loginAction) Execute(_ context.Context, ec *action.ExecContext) (action.Result, error) {
	ec.Log(slog.LevelInfo, "login "+a.User+" with "+a.Password)
	if a.User == "fail" {
		return action.Result{}, errors.New("wrong password " + a.Password)
	}
	return action.Result{Output: map[string]any{"session": "s-" + a.Password}}, nil
}

func secretRegistry() *action.Registry {
	reg := actions.Builtin()
	reg.Register(action.Definition{
		Type: "test.login", Category: action.CategoryGeneral, Label: "Login", Description: "Logs in for a test",
		Config: loginAction{},
		New: func(with map[string]any) (action.Action, error) {
			a := &loginAction{}
			return a, action.DecodeConfig(with, a)
		},
	})
	return reg
}

const loginWorkflow = `kind: Workflow
apiVersion: runnel/v1
name: login
actions:
  - id: start
    type: core.start
  - id: login
    type: test.login
    with:
      user: ${USER}
      password: ${secret:RUNNEL_TEST_PW}
outputs:
  - name: SESSION
    value: "{{ results.login.output.session }}"
links:
  - { from: start, to: login }
`

func prepareLogin(t *testing.T, project, wf string) (*Prepared, *recorder, error) {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "project.yaml"), project)
	rec := &recorder{}
	p, err := Prepare(Spec{Name: "workflows/login.workflow.yaml", Data: []byte(wf), ProjectDir: dir,
		Params: map[string]string{"USER": "ada"}, Registry: secretRegistry(), Observer: rec.observe})
	return p, rec, err
}

// A secret in a field for secrets is read from its store before the run, and its value
// never shows in the log, the error or the outputs (ADR 0022, S39a).
func TestRunMasksSecrets(t *testing.T) {
	t.Setenv("RUNNEL_TEST_PW", "Pa55word!")
	p, rec, err := prepareLogin(t, "kind: Project\napiVersion: runnel/v1\nname: t\nsecretStore: env\n", loginWorkflow)
	if err != nil {
		t.Fatal(err)
	}
	res := p.Runner.Run(context.Background())
	if res.Status != workflow.Succeeded {
		t.Fatalf("%s: %v", res.Status, res.Err)
	}
	logs := rec.logs()
	if !strings.Contains(logs, "login ada with ***") || strings.Contains(logs, "Pa55word!") {
		t.Errorf("log:\n%s", logs)
	}
	if res.Outputs["SESSION"] != "s-***" {
		t.Errorf("outputs %v", res.Outputs)
	}
	for _, e := range rec.events {
		if e.Kind == workflow.RunFinished && e.Outputs["SESSION"] != "s-***" {
			t.Errorf("event outputs %v", e.Outputs)
		}
	}

	// A failure names the secret only as ***.
	p, _, err = prepareLogin(t, "kind: Project\napiVersion: runnel/v1\nname: t\nsecretStore: env\n", strings.Replace(loginWorkflow, "${USER}", "fail", 1))
	if err != nil {
		t.Fatal(err)
	}
	res = p.Runner.Run(context.Background())
	if res.Status != workflow.Failed || !strings.Contains(res.Err.Error(), "wrong password ***") || strings.Contains(res.Err.Error(), "Pa55word!") {
		t.Errorf("%s: %v", res.Status, res.Err)
	}
}

// A secret that cannot be read stops the run before it starts; the message names the
// place and the secret, never a value.
func TestRunMissingSecret(t *testing.T) {
	_, _, err := prepareLogin(t, "kind: Project\napiVersion: runnel/v1\nname: t\nsecretStore: env\n", loginWorkflow)
	diags, ok := errors.AsType[model.Errors](err)
	if !ok || !strings.Contains(err.Error(), `workflows/login.workflow.yaml:11:17: action "login": password: secret "RUNNEL_TEST_PW": environment variable RUNNEL_TEST_PW is not set`) {
		t.Fatalf("%v (%d)", err, len(diags))
	}
	// Without secretStore, secrets read from the local store, which comes later.
	t.Setenv("RUNNEL_TEST_PW", "Pa55word!")
	_, _, err = prepareLogin(t, "kind: Project\napiVersion: runnel/v1\nname: t\n", loginWorkflow)
	if err == nil || !strings.Contains(err.Error(), "the local secret store is not available yet") {
		t.Errorf("local: %v", err)
	}
}

// Outside fields for secrets, ${secret:…} is an error when checking and is never
// resolved when running, so it cannot reach a log.
func TestSecretOutsideSecretField(t *testing.T) {
	t.Setenv("RUNNEL_TEST_PW", "Pa55word!")
	wf := strings.Replace(loginWorkflow, "user: ${USER}", "user: ${secret:RUNNEL_TEST_PW}", 1)
	w, err := model.ReadWorkflow("w.workflow.yaml", []byte(wf))
	if err != nil {
		t.Fatal(err)
	}
	p := &model.Project{SecretStore: "env"}
	diags, err := CheckWorkflow(w, nil, WorkflowCheck{Variables: true, Project: p, Registry: secretRegistry(), Params: map[string]string{}})
	if err != nil || !strings.Contains(diags.Error(), "w.workflow.yaml:10:13: ${secret:RUNNEL_TEST_PW} is not allowed here") {
		t.Errorf("check: %v %v", diags, err)
	}

	// The run does not start.
	_, _, err = prepareLogin(t, "kind: Project\napiVersion: runnel/v1\nname: t\nsecretStore: env\n", wf)
	if err == nil || !strings.Contains(err.Error(), "workflows/login.workflow.yaml:10:13: ${secret:RUNNEL_TEST_PW} is not allowed here") {
		t.Errorf("prepare: %v", err)
	}
}
