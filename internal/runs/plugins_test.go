package runs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/plugin"
	"github.com/runnelhq/runnel/internal/plugin/plugintest"
)

func TestMain(m *testing.M) {
	plugintest.ServeIfPlugin() // the test binary is also the plugin of the tests (S49b2)
	os.Exit(m.Run())
}

const pluginWorkflow = `kind: Workflow
apiVersion: runnel/v1
name: plugin
outputs:
  - name: OUT
    value: "{{ results.echo.output }}"
actions:
  - id: start
    type: core.start
  - id: echo
    type: demo.echo
    with:
      text: ${GREETING}
      api: crm
links:
  - { from: start, to: echo }
`

// pluginProject is a project with the test binary as plugin demo, a connection with a
// secret and an http trigger whose token is in HOOK_TOKEN.
func pluginProject(t testing.TB) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(plugintest.ModeEnv, "normal")
	cfg := t.TempDir() // approvals of the user in a folder of the test
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("APPDATA", cfg)
	t.Setenv("HOOK_TOKEN", "hook-secret")
	dir := t.TempDir()
	for name, content := range map[string]string{
		"project.yaml":                   "kind: Project\napiVersion: runnel/v1\nname: p\nvariables:\n  GREETING: hi\nsecretStore: inline\nsecrets:\n  crm-token: tok-12345\n",
		"connections/crm.yaml":           "kind: Connection\napiVersion: runnel/v1\nname: crm\ntype: http\nwith:\n  baseUrl: https://crm.example.com/\n  auth: bearer\n  token: ${secret:crm-token}\n",
		"triggers/hook.yaml":             "kind: Trigger\napiVersion: runnel/v1\nname: hook\ntype: http\ntarget: workflows/plugin.workflow.yaml\nwith:\n  method: POST\n  path: /hook\n  auth:\n    token: ${env:HOOK_TOKEN}\n",
		"workflows/plugin.workflow.yaml": pluginWorkflow,
		"plugins/demo/plugin.yaml":       "kind: Plugin\napiVersion: runnel/v1\nname: demo\nversion: 1.0.0\nnamespace: demo\nexecutable: '" + exe + "'\n",
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func trustDemo(t testing.TB, dir string) {
	t.Helper()
	store, err := plugin.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Check(dir, "demo", filepath.Join(dir, "plugins", "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Trust(dir, "demo", "1.0.0", st.Hash, false, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func prepareDemo(dir string, observe ...func(workflow.Event)) (*Prepared, error) {
	file := filepath.Join(dir, "workflows", "plugin.workflow.yaml")
	data, _ := os.ReadFile(file)
	spec := Spec{Name: "workflows/plugin.workflow.yaml", File: file, Data: data, ProjectDir: dir, Registry: actions.Builtin()}
	if len(observe) > 0 {
		spec.Observer = observe[0]
	}
	return Prepare(spec)
}

// An action of a trusted plugin runs in a process of the plugin for the run: with the
// configuration resolved (variables, the connection with its secret), its output as the
// result, secrets masked, without the token of the http trigger in its environment.
func TestPluginAction(t *testing.T) {
	dir := pluginProject(t)
	trustDemo(t, dir)
	var mu sync.Mutex
	var logs []string
	p, err := prepareDemo(dir, func(ev workflow.Event) {
		if ev.Kind == workflow.Log {
			mu.Lock()
			logs = append(logs, ev.ActionID+" "+ev.Level.String()+" "+ev.Message)
			mu.Unlock()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	res := p.Runner.Run(context.Background())
	// stderr of the plugin is in the log of the action that called it.
	mu.Lock()
	if got := strings.Join(logs, "|"); !strings.Contains(got, "echo WARN demo: echo hi with s3cret") {
		t.Errorf("log %q", got)
	}
	mu.Unlock()
	if res.Status != workflow.Succeeded {
		t.Fatalf("run %s: %v", res.Status, res.Err)
	}
	got, ok := res.Outputs["OUT"].(string) // the output as JSON text
	if !ok {
		t.Fatalf("output %#v", res.Outputs["OUT"])
	}
	for _, want := range []string{
		`"echo":"hi"`, `"token":"s3cret"`, // the plugin's own text is not a secret of the run
		`"api":{"auth":"bearer","baseUrl":"https://crm.example.com/","name":"crm","token":"***","type":"http"}`,
		`"hookToken":""`, `"dir":` + mustJSON(dir),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// Without approval, after a change and with a plugin that cannot start, the run does not
// start and validate names the reason at the action (ADR 0019).
func TestPluginNotUsable(t *testing.T) {
	dir := pluginProject(t)
	want := `action "echo": plugin "demo" is not trusted on this computer; check it and run "runnel plugins trust"`
	if _, err := prepareDemo(dir); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("not trusted: %v", err)
	}
	w, err := model.LoadWorkflow(filepath.Join(dir, "workflows", "plugin.workflow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	diags, err := CheckWorkflow(w, nil, WorkflowCheck{Registry: actions.Builtin(), ProjectDir: dir})
	if err != nil || !strings.Contains(diags.Error(), want) {
		t.Errorf("validate: %v %v", diags, err)
	}

	trustDemo(t, dir)
	if _, err := prepareDemo(dir); err != nil {
		t.Fatalf("trusted: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugins", "demo", "extra.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareDemo(dir); err == nil || !strings.Contains(err.Error(), `plugin "demo" changed since it was trusted`) {
		t.Errorf("changed: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "plugins", "demo", "extra.txt")); err != nil {
		t.Fatal(err)
	}
	t.Setenv(plugintest.ModeEnv, "v2")
	trustDemo(t, dir) // the folder changed back; approve it again so that the cache is new
	if _, err := prepareDemo(dir); err == nil || !strings.Contains(err.Error(), "protocol version 2 is not supported") {
		t.Errorf("cannot start: %v", err)
	}
}

// The configuration is checked against the schema of describe before the run.
func TestPluginConfig(t *testing.T) {
	dir := pluginProject(t)
	trustDemo(t, dir)
	file := filepath.Join(dir, "workflows", "plugin.workflow.yaml")
	for _, tt := range []struct{ old, new, want string }{
		{"      text: ${GREETING}\n", "", `missing required field "text"`},
		{"      api: crm\n", "      api: crm\n      colour: red\n", `unknown field "colour"`},
		{"      api: crm\n", "      api: nope\n", `connection "nope"`},
	} {
		if err := os.WriteFile(file, []byte(strings.Replace(pluginWorkflow, tt.old, tt.new, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := prepareDemo(dir); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%q: %v", tt.want, err)
		}
	}
}

// writeFile writes a file of the project in dir.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const demoConnection = "kind: Connection\napiVersion: runnel/v1\nname: svc\ntype: demo\nwith:\n  host: %s\n  password: ${secret:svc-pass}\n"

// demoProject is pluginProject with a connection svc of the plugin's type demo.
func demoProject(t *testing.T, host string) string {
	t.Helper()
	dir := pluginProject(t)
	writeFile(t, dir, "project.yaml", "kind: Project\napiVersion: runnel/v1\nname: p\nvariables:\n  GREETING: hi\nsecretStore: inline\nsecrets:\n  crm-token: tok-12345\n  svc-pass: pw-98765\n")
	writeFile(t, dir, "connections/svc.yaml", strings.Replace(demoConnection, "%s", host, 1))
	return dir
}

func checkSvc(t *testing.T, dir string) string {
	t.Helper()
	p, err := model.LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	return CheckConnection(p.Connections["svc"], p, "").Error()
}

// A connection of a type of a plugin is checked against the schema of describe, with its
// secret fields; without approval the check names the reason at type (S49c).
func TestPluginConnectionType(t *testing.T) {
	dir := demoProject(t, "db.example.com")
	if got := checkSvc(t, dir); !strings.Contains(got, `4:7: plugin "demo" is not trusted on this computer`) {
		t.Errorf("not trusted: %q", got)
	}
	trustDemo(t, dir)
	if got := checkSvc(t, dir); got != "" {
		t.Errorf("trusted: %q", got)
	}
	for _, tt := range []struct{ old, new, want string }{
		{"  host: db.example.com\n", "", `demo connection: missing required field "host"`},
		{"  host: db.example.com\n", "  host: db.example.com\n  port: 1\n", `demo connection: unknown field "port"`},
		{"  host: db.example.com\n", "  host: ${secret:svc-pass}\n", "secret"},
		{"type: demo\n", "type: demo-other\n", `unknown connection type "demo-other"`},
	} {
		writeFile(t, dir, "connections/svc.yaml", strings.Replace(strings.Replace(demoConnection, "%s", "db.example.com", 1), tt.old, tt.new, 1))
		if got := checkSvc(t, dir); !strings.Contains(got, tt.want) {
			t.Errorf("%q: %q", tt.want, got)
		}
	}
}

// connection.test of the plugin tests a connection of its type; secrets are masked.
func TestPluginConnectionTest(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct{ host, want, err string }{
		{"db.example.com", "connected to db.example.com as *** demo 1.0", ""},
		{"down", "", "login with *** refused"},
	} {
		dir := demoProject(t, tt.host)
		p, err := model.LoadProject(dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := ResolveConnection(ctx, p, "", "svc"); err == nil || !strings.Contains(err.Error(), "not trusted") {
			t.Errorf("not trusted: %v", err)
		}
		trustDemo(t, dir)
		// The catalog tells that the type has a test (key test, E15e).
		if d, _ := ConnectionTypes(dir).Descriptor("demo"); d.Test != plugin.PluginConnectionTest {
			t.Errorf("test of the type: %q", d.Test)
		}
		c, mask, err := ResolveConnection(ctx, p, "", "svc")
		if err != nil {
			t.Fatal(err)
		}
		got, err := TestConnection(ctx, dir, c, mask)
		if got != tt.want || (tt.err == "") != (err == nil) || (err != nil && err.Error() != tt.err) {
			t.Errorf("%s: %q %v", tt.host, got, err)
		}
	}
}

// An action of a plugin gets a connection of the plugin's type resolved, its secret masked.
func TestPluginActionWithPluginConnection(t *testing.T) {
	dir := demoProject(t, "db.example.com")
	trustDemo(t, dir)
	writeFile(t, dir, "workflows/plugin.workflow.yaml", strings.Replace(pluginWorkflow, "api: crm", "api: svc", 1))
	p, err := prepareDemo(dir)
	if err != nil {
		t.Fatal(err)
	}
	res := p.Runner.Run(context.Background())
	if res.Status != workflow.Succeeded {
		t.Fatalf("run %s: %v", res.Status, res.Err)
	}
	got, _ := res.Outputs["OUT"].(string)
	if want := `"api":{"host":"db.example.com","name":"svc","password":"***","type":"demo"}`; !strings.Contains(got, want) {
		t.Errorf("missing %s in %s", want, got)
	}
}
