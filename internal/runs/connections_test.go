package runs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/model"
)

func dbProject(t *testing.T, conn string) string {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: t\nsecretStore: env\nvariables:\n  DB_FILE: out/test.db\n")
	writeTestFile(t, filepath.Join(dir, "connections", "app.yaml"), conn)
	return dir
}

const appConnection = "kind: Connection\napiVersion: runnel/v1\nname: app\ntype: sqlite\nwith:\n  path: ${DB_FILE}\n"

const dbWorkflow = `kind: Workflow
apiVersion: runnel/v1
name: db
actions:
  - id: start
    type: core.start
  - id: write
    type: db.execute
    with:
      connection: app
      sql: CREATE TABLE IF NOT EXISTS t (n INTEGER)
links:
  - { from: start, to: write }
`

// A workflow writes through a connection of the project; the path of the database
// comes from a variable and is relative to the project folder.
func TestRunWithConnection(t *testing.T) {
	dir := dbProject(t, appConnection)
	p, err := Prepare(Spec{Name: "workflows/db.workflow.yaml", Data: []byte(dbWorkflow), ProjectDir: dir, Registry: actions.Builtin()})
	if err != nil {
		t.Fatal(err)
	}
	if res := p.Runner.Run(context.Background()); res.Status != workflow.Succeeded {
		t.Fatalf("%s: %v", res.Status, res.Err)
	}
	if _, err := model.LoadProject(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "out", "test.db")); err != nil {
		t.Errorf("database file not in the project folder: %v", err)
	}
}

// Problems with connections stop the run before it starts, with the place.
func TestRunConnectionProblems(t *testing.T) {
	for _, tt := range []struct {
		conn, wf, want string
	}{
		{appConnection, strings.Replace(dbWorkflow, "connection: app", "connection: shop", 1),
			`workflows/db.workflow.yaml:10:19: action "write": connection "shop" not found`},
		{appConnection, strings.Replace(dbWorkflow, "CREATE TABLE IF NOT EXISTS t (n INTEGER)", "DELETE FROM t WHERE n = ${N}", 1),
			"workflows/db.workflow.yaml:11:12: the statement must not contain ${…} or {{ … }}"},
		{strings.Replace(appConnection, "${DB_FILE}", "${MISSING}", 1), dbWorkflow,
			`action "write": connection app: path: variable "MISSING" is undefined`},
	} {
		dir := dbProject(t, tt.conn)
		_, err := Prepare(Spec{Name: "workflows/db.workflow.yaml", Data: []byte(tt.wf), ProjectDir: dir, Registry: actions.Builtin()})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v\nwant %q", err, tt.want)
		}
	}
}

// Connection files are checked against their type (runnel validate and the editor).
func TestCheckConnection(t *testing.T) {
	for conn, want := range map[string]string{
		appConnection: "",
		strings.Replace(appConnection, "type: sqlite", "type: mysql", 1):            `connections/app.yaml:4:7: unknown connection type "mysql" (available: http, postgres, smtp, sqlite, sqlserver)`,
		strings.Replace(appConnection, "path: ${DB_FILE}", "file: x.db", 1):         `connections/app.yaml:6:9: sqlite connection: unknown field "file"`,
		strings.Replace(appConnection, "path: ${DB_FILE}", "path: ${secret:pw}", 1): `connections/app.yaml:6:9: ${secret:pw} is not allowed here`,
		strings.Replace(appConnection, "path: ${DB_FILE}", "path: ${NOT_SET}", 1):   `warning: variable "NOT_SET" is not defined`,
		strings.Replace(appConnection, "path: ${DB_FILE}", "readOnly: true", 1):     `missing required field "path"`,
	} {
		dir := dbProject(t, conn)
		p, err := model.LoadProject(dir)
		if err != nil {
			t.Fatal(err)
		}
		got := CheckConnections(p, "").Error()
		if want == "" && got != "" || want != "" && !strings.Contains(got, want) {
			t.Errorf("%q:\n%s\nwant %q", conn, got, want)
		}
	}
}

// validate: connections named by actions exist and have a fitting type; names with ${…}
// are checked when the run starts.
func TestCheckConnectionRefs(t *testing.T) {
	dir := dbProject(t, appConnection)
	p, err := model.LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	for wf, want := range map[string]string{
		dbWorkflow: "",
		strings.Replace(dbWorkflow, "connection: app", "connection: other", 1):   `connection "other" not found`,
		strings.Replace(dbWorkflow, "connection: app", "connection: ${CONN}", 1): "the connection is only known when the run starts",
	} {
		w, err := model.ReadWorkflow("w.workflow.yaml", []byte(wf))
		if err != nil {
			t.Fatal(err)
		}
		diags, _ := CheckWorkflow(w, nil, WorkflowCheck{Variables: true, Project: p, Registry: actions.Builtin(), Params: map[string]string{"CONN": "app"}})
		got := diags.Error()
		if want == "" && got != "" || want != "" && !strings.Contains(got, want) {
			t.Errorf("%s\nwant %q", got, want)
		}
	}
}

// Secrets of a connection and the values of ${env:…} in it are masked in the run.
func TestConnectionResolverMasks(t *testing.T) {
	t.Setenv("RUNNEL_TEST_DBPW", "db-Pa55word")
	t.Setenv("RUNNEL_TEST_DBDIR", "secret-folder")
	p := &model.Project{SecretStore: "env", Connections: map[string]*model.Connection{}}
	c, err := model.ReadConnection("connections/app.yaml", []byte("kind: Connection\napiVersion: runnel/v1\nname: app\ntype: sqlite\nwith:\n  path: ${env:RUNNEL_TEST_DBDIR}/x.db\n"))
	if err != nil {
		t.Fatal(err)
	}
	p.Connections["app"] = c
	_, mask := secretScope(model.Scope(&model.Workflow{}, p, nil, nil))
	got, err := connectionResolver(p, nil, mask)(context.Background(), "app")
	if err != nil || got.With["path"] != "secret-folder/x.db" {
		t.Fatalf("%+v, %v", got, err)
	}
	if m := mask.String("open secret-folder/x.db"); m != "open ***/x.db" {
		t.Errorf("mask: %q", m)
	}
}

// Switched off certificate checks and unencrypted connections are warnings (ADR 0022).
func TestCheckConnectionWarnings(t *testing.T) {
	for conn, want := range map[string]string{
		"kind: Connection\napiVersion: runnel/v1\nname: app\ntype: sqlserver\nwith:\n  host: db\n  database: d\n  user: u\n  trustServerCertificate: true\n": "connections/app.yaml:9:27: warning: the certificate of the server is not checked",
		"kind: Connection\napiVersion: runnel/v1\nname: app\ntype: sqlserver\nwith:\n  host: db\n  database: d\n  user: u\n  encrypt: \"false\"\n":           "warning: the connection is not encrypted (only the login is)",
		"kind: Connection\napiVersion: runnel/v1\nname: app\ntype: postgres\nwith:\n  host: db\n  database: d\n  user: u\n  sslMode: disable\n":              "warning: the connection is not encrypted",
		"kind: Connection\napiVersion: runnel/v1\nname: app\ntype: postgres\nwith:\n  host: db\n  database: d\n  user: u\n":                                  "",
	} {
		dir := dbProject(t, conn)
		p, err := model.LoadProject(dir)
		if err != nil {
			t.Fatal(err)
		}
		got := CheckConnections(p, "").Error()
		if want == "" && got != "" || want != "" && !strings.Contains(got, want) {
			t.Errorf("%s\nwant %q", got, want)
		}
	}
}

// Mappings of a connection (headers of http) are resolved value by value; a secret
// field may also be a mapping of texts with ${secret:…}, which are masked.
func TestConnectionMappings(t *testing.T) {
	t.Setenv("RUNNEL_TEST_APIKEY", "k-Pa55word")
	const conn = "kind: Connection\napiVersion: runnel/v1\nname: app\ntype: http\nwith:\n  baseUrl: https://${HOST}/v1\n  headers:\n    X-Key: ${secret:RUNNEL_TEST_APIKEY}\n    Accept: application/json\n"
	p := &model.Project{SecretStore: "env", Variables: map[string]string{"HOST": "api.example.com"}, Connections: map[string]*model.Connection{}}
	c, err := model.ReadConnection("connections/app.yaml", []byte(conn))
	if err != nil {
		t.Fatal(err)
	}
	if errs := c.CheckSecrets(func(field string) bool { return builtinConnectionTypes.SecretField(c.Type, field) }, nil); len(errs) > 0 {
		t.Fatalf("secret in headers refused: %v", errs)
	}
	p.Connections["app"] = c
	_, mask := secretScope(model.Scope(&model.Workflow{}, p, nil, nil))
	got, err := connectionResolver(p, nil, mask)(context.Background(), "app")
	if err != nil {
		t.Fatal(err)
	}
	headers, _ := got.With["headers"].(map[string]any)
	if got.With["baseUrl"] != "https://api.example.com/v1" || headers["X-Key"] != "k-Pa55word" || headers["Accept"] != "application/json" {
		t.Fatalf("%+v", got.With)
	}
	if m := mask.String("key k-Pa55word"); m != "key ***" {
		t.Errorf("mask: %q", m)
	}
	// Other fields stay without secrets.
	bad, err := model.ReadConnection("connections/app.yaml", []byte(strings.Replace(conn, "https://${HOST}/v1", "${secret:RUNNEL_TEST_APIKEY}", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if errs := bad.CheckSecrets(func(field string) bool { return builtinConnectionTypes.SecretField(bad.Type, field) }, nil); len(errs) != 1 || !strings.Contains(errs.Error(), "is not allowed here") {
		t.Errorf("%v", errs)
	}
}
