package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testConnection(t *testing.T, s *Server, name, body string) (testResult, int) {
	t.Helper()
	rec := do(t, s, "POST", "/api/v1/connections/"+name+"/test", body)
	var res testResult
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("%s: %v", rec.Body, err)
		}
	}
	return res, rec.Code
}

// POST /connections/{name}/test (S39c1): a saved connection, resolved for an
// environment; a failed test is an answer with ok false.
func TestTestConnection(t *testing.T) {
	dir, s := newProject(t)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer tok-Pa55":
			_, _ = w.Write([]byte("ok"))
		case "Bearer slow":
			time.Sleep(300 * time.Millisecond)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	t.Cleanup(api.Close)
	t.Setenv("RUNNEL_TEST_API_TOKEN", "tok-Pa55")
	conn := func(name, typ, with string) {
		writeFile(t, filepath.Join(dir, "connections", name+".yaml"), "kind: Connection\napiVersion: runnel/v1\nname: "+name+"\ntype: "+typ+"\nwith:\n"+with)
	}
	conn("db", "sqlite", "  path: out/app.db\n")
	conn("api", "http", "  baseUrl: "+api.URL+"\n  auth: bearer\n  token: ${secret:env/RUNNEL_TEST_API_TOKEN}\n")
	conn("refused", "http", "  baseUrl: "+api.URL+"\n  auth: bearer\n  token: wrong\n")
	conn("slow", "http", "  baseUrl: "+api.URL+"\n  auth: bearer\n  token: slow\n")
	conn("missing", "http", "  baseUrl: "+api.URL+"\n  auth: bearer\n  token: ${secret:env/RUNNEL_TEST_NOT_SET}\n")

	// SQLite: the test does not create the file.
	res, code := testConnection(t, s, "db", `{"environment": "dev"}`)
	if code != 200 || !res.OK || !strings.Contains(res.Message, "does not exist yet") || res.Environment != "dev" {
		t.Fatalf("%d %+v", code, res)
	}
	if _, err := filepath.Glob(filepath.Join(dir, "out", "*")); err != nil {
		t.Fatal(err)
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "out", "app.db")); len(files) != 0 {
		t.Error("the test created the database file")
	}
	if res, _ := testConnection(t, s, "api", ""); !res.OK || !strings.HasSuffix(res.Message, "/ answered 200 OK") {
		t.Errorf("%+v", res)
	}
	if res, _ := testConnection(t, s, "refused", ""); res.OK || !strings.Contains(res.Message, "refused the sign-in") {
		t.Errorf("%+v", res)
	}
	if res, _ := testConnection(t, s, "missing", ""); res.OK || !strings.Contains(res.Message, "RUNNEL_TEST_NOT_SET is not set") {
		t.Errorf("%+v", res)
	}
	old := testTimeout
	testTimeout = 100 * time.Millisecond
	t.Cleanup(func() { testTimeout = old })
	if res, _ := testConnection(t, s, "slow", ""); res.OK || res.Message != "no answer within 100ms" {
		t.Errorf("%+v", res)
	}

	// Refusals.
	expect(t, do(t, s, "POST", "/api/v1/connections/nope/test", ""), http.StatusNotFound, `connection \"nope\" not found`)
	expect(t, do(t, s, "POST", "/api/v1/connections/db/test", `{"environment": "qa"}`), http.StatusBadRequest, `environment \"qa\" not found`)
	expect(t, do(t, s, "POST", "/api/v1/connections/db/test", `{"environment": `), http.StatusBadRequest, "invalid request")
	s.testing.Store("db", true)
	expect(t, do(t, s, "POST", "/api/v1/connections/db/test", ""), http.StatusConflict, "is being tested already")
	s.testing.Delete("db")
	expect(t, do(t, s, "POST", "/api/v1/connections/db/test", "", "Origin", "http://evil.example"), http.StatusForbidden, "other origins")
}

// GET /secrets (S39c1): every ${secret:…} with its uses and its state per environment,
// never a value.
func TestSecretsList(t *testing.T) {
	dir, s := newProject(t)
	t.Setenv("RUNNEL_TEST_DB_PW", "Pa55-db")
	writeFile(t, filepath.Join(dir, "environments", "dev.yaml"), "kind: Environment\napiVersion: runnel/v1\nname: dev\nsecretStore: env\n")
	writeFile(t, filepath.Join(dir, "environments", "prod.yaml"), "kind: Environment\napiVersion: runnel/v1\nname: prod\nsecretStore: inline\nsecrets:\n  RUNNEL_TEST_DB_PW: Inline-Pa55\n")
	writeFile(t, filepath.Join(dir, "connections", "db.yaml"), "kind: Connection\napiVersion: runnel/v1\nname: db\ntype: postgres\nwith:\n  host: h\n  database: d\n  user: u\n  password: ${secret:RUNNEL_TEST_DB_PW}\n")
	writeFile(t, filepath.Join(dir, "connections", "api.yaml"), "kind: Connection\napiVersion: runnel/v1\nname: api\ntype: http\nwith:\n  baseUrl: https://h\n  auth: bearer\n  token: ${secret:local/api-token}\n")
	rec := do(t, s, "GET", "/api/v1/secrets", "")
	if strings.Contains(rec.Body.String(), "Pa55") {
		t.Fatal("a value of a secret in the answer")
	}
	var info secretsInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if strings.Join(info.Environments, " ") != "dev prod" || len(info.Secrets) != 2 {
		t.Fatalf("%+v", info)
	}
	pw, token := info.Secrets[0], info.Secrets[1]
	if pw.Name != "RUNNEL_TEST_DB_PW" || pw.Uses[0].File != "connections/db.yaml" || pw.Uses[0].Line != 9 ||
		pw.Environments["dev"] != (secretState{Store: "env", State: "set"}) || pw.Environments["prod"] != (secretState{Store: "inline", State: "set"}) {
		t.Errorf("%+v", pw)
	}
	if token.Name != "api-token" || token.Store != "local" || token.Environments["dev"] != (secretState{Store: "local", State: "unavailable"}) {
		t.Errorf("%+v", token)
	}
	t.Setenv("RUNNEL_TEST_DB_PW", "")
	_ = json.Unmarshal(do(t, s, "GET", "/api/v1/secrets", "").Body.Bytes(), &info)
	if info.Secrets[0].Environments["dev"].State != "missing" {
		t.Errorf("%+v", info.Secrets[0])
	}
}

// POST /connections/edit (S39c2a): reads a connection for the form and sets one field
// in its text, keeping the rest.
func TestEditConnectionForm(t *testing.T) {
	_, s := newProject(t)
	text := "kind: Connection\napiVersion: runnel/v1\nname: db\ntype: sqlite\nwith:\n  path: out/app.db  # the file\n"
	body := func(set string) string {
		b, _ := json.Marshal(map[string]any{"path": "connections/db.yaml", "content": text})
		if set == "" {
			return string(b)
		}
		return strings.TrimSuffix(string(b), "}") + `,"set":` + set + "}"
	}
	var res connectionEditResponse
	rec := do(t, s, "POST", "/api/v1/connections/edit", body(""))
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || res.Connection == nil || res.Connection.Type != "sqlite" || res.Connection.With["path"] != "out/app.db" || res.Content != text {
		t.Fatalf("%s %v", rec.Body, err)
	}
	rec = do(t, s, "POST", "/api/v1/connections/edit", body(`{"field":"with.readOnly","value":true}`))
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || !strings.HasSuffix(res.Content, "  path: out/app.db  # the file\n  readOnly: true\n") || res.Connection.With["readOnly"] != true {
		t.Fatalf("%s %v", rec.Body, err)
	}
	expect(t, do(t, s, "POST", "/api/v1/connections/edit", body(`{"field":"name","value":"x"}`)), http.StatusBadRequest, "cannot be set")
	expect(t, do(t, s, "POST", "/api/v1/connections/edit", `{"path":"workflows/a.workflow.yaml","content":""}`), http.StatusBadRequest, "only connection files")
	// A file that does not read has no connection for the form.
	if rec := do(t, s, "POST", "/api/v1/connections/edit", `{"path":"connections/db.yaml","content":"kind: ["}`); rec.Code != 200 || strings.Contains(rec.Body.String(), `"connection"`) {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	expect(t, do(t, s, "POST", "/api/v1/connections/edit", `{"path":"../x.yaml","content":""}`), http.StatusBadRequest, "")
	expect(t, do(t, s, "POST", "/api/v1/connections/edit", body(""), "Origin", "http://evil.example"), http.StatusForbidden, "other origins")
}
