package server

import (
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
