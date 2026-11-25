package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/plugin"
	"github.com/runnelhq/runnel/internal/plugin/plugintest"
)

func TestMain(m *testing.M) {
	plugintest.ServeIfPlugin() // the test binary is also the plugin of the tests (S49c)
	os.Exit(m.Run())
}

// addDemoPlugin adds the test binary as plugin demo to the project in dir, trusted in a
// folder of the test, and a connection svc of its type demo.
func addDemoPlugin(t testing.TB, dir, host string) {
	t.Helper()
	writeDemoPlugin(t, dir, host)
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

// writeDemoPlugin adds the files of the plugin and the connection, without approval.
func writeDemoPlugin(t testing.TB, dir, host string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(plugintest.ModeEnv, "normal")
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("APPDATA", cfg)
	t.Setenv("RUNNEL_TEST_SVC_PASS", "pw-98765")
	writeFile(t, filepath.Join(dir, "plugins", "demo", "plugin.yaml"),
		"kind: Plugin\napiVersion: runnel/v1\nname: demo\nversion: 1.0.0\nnamespace: demo\nexecutable: '"+exe+"'\n")
	writeFile(t, filepath.Join(dir, "connections", "svc.yaml"),
		"kind: Connection\napiVersion: runnel/v1\nname: svc\ntype: demo\nwith:\n  host: "+host+"\n  password: ${secret:env/RUNNEL_TEST_SVC_PASS}\n")
}

// The schema of connection files offers the connection types of trusted plugins, and a
// connection of such a type is tested by its plugin, secrets masked (S49c).
func TestPluginConnection(t *testing.T) {
	dir, s := newProject(t)
	addDemoPlugin(t, dir, "db.example.com")
	rec := do(t, s, "GET", "/api/v1/schemas/connection", "")
	if body := rec.Body.String(); !strings.Contains(body, `"demo"`) || !strings.Contains(body, `"description": "Demo: A demo service"`) {
		t.Errorf("schema: %s", body)
	}
	if res, _ := testConnection(t, s, "svc", ""); !res.OK || res.Message != "connected to db.example.com as *** demo 1.0" {
		t.Errorf("%+v", res)
	}
	writeFile(t, filepath.Join(dir, "connections", "svc.yaml"),
		"kind: Connection\napiVersion: runnel/v1\nname: svc\ntype: demo\nwith:\n  host: down\n  password: ${secret:env/RUNNEL_TEST_SVC_PASS}\n")
	if res, _ := testConnection(t, s, "svc", ""); res.OK || res.Message != "login with *** refused" {
		t.Errorf("%+v", res)
	}
}

const countPipeline = `kind: Pipeline
apiVersion: runnel/v1
name: count
transforms:
  - id: count
    type: demo.count
    with:
      count: 3
      api: svc
      fields:
        - { name: n, type: int }
  - id: out
    type: core.dummy
links:
  - { from: count, to: out }
`

// "Detect fields" of a transform of a plugin asks the plugin (transform.detectFields)
// with its connection resolved, compares with fields: and masks secrets in errors (S50b).
func TestPluginDetectFields(t *testing.T) {
	s := pipelineServer(t)
	addDemoPlugin(t, s.dir, "db.example.com")
	rec := do(t, s, "POST", "/api/v1/pipelines/fields", fieldsBody(t, countPipeline, "count"))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var res struct {
		Source []struct{ Name, Value string }
		Fields []struct {
			Name, Type, State, Note string
			Selected                bool
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	// The plugin gets the connection with its secret; this test plugin shows it.
	if len(res.Source) != 1 || res.Source[0].Value != "svc db.example.com pw-98765" {
		t.Errorf("source %+v", res.Source)
	}
	if len(res.Fields) != 2 || res.Fields[0].State != "listed" || !res.Fields[0].Selected || res.Fields[1].State != "new" || res.Fields[1].Note != "limit 1000" {
		t.Errorf("fields %+v", res.Fields)
	}
	rec = do(t, s, "POST", "/api/v1/pipelines/fields", fieldsBody(t, strings.Replace(countPipeline, "api: svc", "api: nope", 1), "count"))
	expect(t, rec, http.StatusUnprocessableEntity, `connection \"nope\"`)
}

type pluginsBody struct {
	Plugins  []pluginInfo `json:"plugins"`
	CanTrust bool         `json:"canTrust"`
}

func getPlugins(t *testing.T, s *Server) pluginsBody {
	t.Helper()
	rec := do(t, s, "GET", "/api/v1/plugins", "")
	var b pluginsBody
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &b) != nil || len(b.Plugins) != 1 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	return b
}

func postPlugin(t *testing.T, s *Server, action, body string) pluginInfo {
	t.Helper()
	rec := do(t, s, "POST", "/api/v1/plugins/demo/"+action, body)
	var p pluginInfo
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &p) != nil {
		t.Fatalf("%s: %d %s", action, rec.Code, rec.Body)
	}
	return p
}

// GET /plugins lists the plugins with their state, types once trusted and their uses;
// POST …/trust approves the plugin as shown (hash), …/untrust takes it back, …/reload
// checks and describes it again (S52a).
func TestPluginsAPI(t *testing.T) {
	dir, s := newProject(t)
	writeDemoPlugin(t, dir, "db.example.com")
	writeFile(t, filepath.Join(dir, "workflows", "check.workflow.yaml"),
		"kind: Workflow\napiVersion: runnel/v1\nname: check\nactions:\n  - id: start\n    type: core.start\n  - id: say\n    type: demo.echo\n    with:\n      text: hi\nlinks:\n  - { from: start, to: say }\n")

	b := getPlugins(t, s)
	p := b.Plugins[0]
	if !b.CanTrust || p.State != pluginNotTrusted || p.Hash == "" || p.Files != 1 || p.Types != nil || p.User != nil || p.Folder != "plugins/demo" || p.Program == "" {
		t.Fatalf("not trusted: %+v", p)
	}
	uses := map[string]bool{}
	for _, u := range p.Uses {
		uses[u.Kind+" "+u.ID+" "+u.Type+" "+u.File] = true
	}
	if !uses["action say demo.echo workflows/check.workflow.yaml"] || !uses["connection svc demo connections/svc.yaml"] || len(p.Uses) != 2 {
		t.Errorf("uses %+v", p.Uses)
	}

	// Trust only as shown: without hash or with another one it is refused.
	expect(t, do(t, s, "POST", "/api/v1/plugins/demo/trust", `{}`), http.StatusBadRequest, "hash is missing")
	expect(t, do(t, s, "POST", "/api/v1/plugins/demo/trust", `{"hash":"0000"}`), http.StatusConflict, "changed since it was shown")
	expect(t, do(t, s, "POST", "/api/v1/plugins/demo/trust", `{"hash":`), http.StatusBadRequest, "invalid request")
	p = postPlugin(t, s, "trust", `{"hash":"`+p.Hash+`"}`)
	if p.State != pluginTrusted || p.User == nil || !p.User.Current || p.User.Version != "1.0.0" || p.Types == nil {
		t.Fatalf("trusted: %+v", p)
	}
	if len(p.Types.Actions) != 1 || p.Types.Actions[0].Type != "demo.echo" || len(p.Types.Transforms) != 2 || len(p.Types.ConnectionTypes) != 1 {
		t.Errorf("types %+v", p.Types)
	}

	// A change of a file voids the approval.
	writeFile(t, filepath.Join(dir, "plugins", "demo", "extra.txt"), "x")
	p = getPlugins(t, s).Plugins[0]
	if p.State != pluginChanged || p.User == nil || p.User.Current || p.Types != nil || p.Files != 2 {
		t.Fatalf("changed: %+v", p)
	}
	p = postPlugin(t, s, "trust", `{"hash":"`+p.Hash+`"}`)

	// A plugin that cannot start; reload tries again.
	t.Setenv(plugintest.ModeEnv, "v2")
	if p = postPlugin(t, s, "reload", ""); p.State != pluginError || !strings.Contains(p.Message, "protocol version 2 is not supported") {
		t.Fatalf("error: %+v", p)
	}
	t.Setenv(plugintest.ModeEnv, "normal")
	if p = getPlugins(t, s).Plugins[0]; p.State != pluginError {
		t.Errorf("cached: %+v", p)
	}
	if p = postPlugin(t, s, "reload", ""); p.State != pluginTrusted {
		t.Errorf("reloaded: %+v", p)
	}

	if p = postPlugin(t, s, "untrust", ""); p.State != pluginNotTrusted || p.User != nil {
		t.Errorf("untrusted: %+v", p)
	}

	// Refusals.
	expect(t, do(t, s, "POST", "/api/v1/plugins/nope/trust", `{"hash":"x"}`), http.StatusNotFound, `plugin \"nope\" not found`)
	expect(t, do(t, s, "POST", "/api/v1/plugins/demo/trust", `{"hash":"x"}`, "Origin", "http://evil.example"), http.StatusForbidden, "other origins")
	expect(t, do(t, s, "GET", "/api/v1/plugins", "", "Origin", "http://evil.example"), http.StatusForbidden, "other origins")
}

// Without plugins the list is empty.
func TestPluginsEmpty(t *testing.T) {
	_, s := newProject(t)
	expect(t, do(t, s, "GET", "/api/v1/plugins", ""), http.StatusOK, `"plugins": []`)
}

// BenchmarkPlugins lists a trusted plugin with a program of 20 MB in its folder, as the
// editor does on every visit of the explorer (S52a).
func BenchmarkPlugins(b *testing.B) {
	dir, s := newProject(b)
	writeFile(b, filepath.Join(dir, "plugins", "demo", "bin", "demo.exe"), strings.Repeat("x", 20<<20))
	addDemoPlugin(b, dir, "db.example.com")
	b.ReportAllocs()
	for b.Loop() {
		rec := do(b, s, "GET", "/api/v1/plugins", "")
		if rec.Code != http.StatusOK {
			b.Fatal(rec.Body)
		}
	}
}
