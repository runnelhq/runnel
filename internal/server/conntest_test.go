package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/secretstore"
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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // an empty local secret store
	t.Setenv("LOCALAPPDATA", t.TempDir())
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
	if token.Name != "api-token" || token.Store != "local" || token.Environments["dev"] != (secretState{Store: "local", State: "missing"}) {
		t.Errorf("%+v", token)
	}
	t.Setenv("RUNNEL_TEST_DB_PW", "")
	// Set in the local store of the user (S39d); the answer still holds no value.
	t.Setenv(secretstore.MasterKeyEnv, base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789ABCDEF")))
	local, err := secretstore.User()
	if err != nil {
		t.Fatal(err)
	}
	if err := local.Set(dir, "api-token", "T0ken-local", time.Now()); err != nil {
		t.Fatal(err)
	}
	rec = do(t, s, "GET", "/api/v1/secrets", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &info)
	if info.Secrets[0].Environments["dev"].State != "missing" || info.Secrets[1].Environments["prod"] != (secretState{Store: "local", State: "set"}) ||
		strings.Contains(rec.Body.String(), "T0ken-local") {
		t.Errorf("%+v", info.Secrets)
	}
}
