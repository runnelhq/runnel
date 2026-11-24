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
func addDemoPlugin(t *testing.T, dir, host string) {
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
