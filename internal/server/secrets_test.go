package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/secretstore"
)

const (
	testKeyA = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlBQkNERUY=" // "0123456789abcdef0123456789ABCDEF"
	testKeyB = "QUJDREVGMDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODk="
)

// secretProject is a project with three environments: dev reads secrets from inline,
// test from env, prod from the local store (as in mockup S39f). The local store of the
// user is empty; the messages of the server log are collected.
func secretProject(t *testing.T) (string, *Server, *logLines) {
	t.Helper()
	dir, s := newProject(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv(secretstore.MasterKeyEnv, testKeyA)
	writeFile(t, filepath.Join(dir, "environments", "dev.yaml"), "kind: Environment\napiVersion: runnel/v1\nname: dev\nsecretStore: inline\nsecrets:\n  erp-password: Inline-Pa55\n")
	writeFile(t, filepath.Join(dir, "environments", "test.yaml"), "kind: Environment\napiVersion: runnel/v1\nname: test\nsecretStore: env\n")
	writeFile(t, filepath.Join(dir, "environments", "prod.yaml"), "kind: Environment\napiVersion: runnel/v1\nname: prod\n")
	writeFile(t, filepath.Join(dir, "connections", "erp.yaml"), "kind: Connection\napiVersion: runnel/v1\nname: erp\ntype: postgres\nwith:\n  host: h\n  database: d\n  user: u\n  password: ${secret:erp-password}\n")
	log := &logLines{}
	s.info, s.warn = log.add, log.add
	return dir, s, log
}

type logLines struct {
	mu    sync.Mutex
	lines []string
}

func (l *logLines) add(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, msg)
}

func (l *logLines) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

func secretsOf(t *testing.T, s *Server) secretsInfo {
	t.Helper()
	rec := do(t, s, "GET", "/api/v1/secrets", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /secrets: %d %s", rec.Code, rec.Body)
	}
	var info secretsInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	return info
}

func errorBody(t *testing.T, body []byte) apiError {
	t.Helper()
	var e apiError
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestSetAndDeleteSecret(t *testing.T) {
	dir, s, log := secretProject(t)
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	info := secretsOf(t, s)
	if got := info.Secrets[0].Environments["prod"]; got != (secretState{Store: "local", State: "missing"}) {
		t.Fatalf("prod before: %+v", got)
	}
	if info.Local.Store != "user" || info.Local.ReadOnly != "" || info.Local.Error != "" || len(info.Local.Unused) != 0 {
		t.Fatalf("local: %+v", info.Local)
	}

	// A multi-line value keeps its line breaks.
	value := "-----BEGIN KEY-----\nS3cr3t-v4lue\n-----END KEY-----\n"
	body, _ := json.Marshal(setSecretRequest{Environment: "prod", Value: value})
	rec := do(t, s, "PUT", "/api/v1/secrets/erp-password", string(body), "Origin", "http://127.0.0.1:7300")
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body)
	}
	var res setSecretResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res != (setSecretResponse{Name: "erp-password", Store: "local", Updated: "2026-11-27T10:00:00Z"}) {
		t.Errorf("answer: %+v", res)
	}
	user, _ := secretstore.User()
	if v, ok, err := user.Get(dir, "erp-password"); err != nil || !ok || v != value {
		t.Fatalf("stored: %q %v %v", v, ok, err)
	}
	info = secretsOf(t, s)
	if got := info.Secrets[0].Environments["prod"]; got != (secretState{Store: "local", State: "set", Updated: "2026-11-27T10:00:00Z"}) {
		t.Errorf("prod after: %+v", got)
	}

	// Replace without an environment: the default store of the project is local.
	now = now.Add(time.Hour)
	rec = do(t, s, "PUT", "/api/v1/secrets/erp-password", `{"value":"N3w-v4lue"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("replace: %d %s", rec.Code, rec.Body)
	}
	if v, _, _ := user.Get(dir, "erp-password"); v != "N3w-v4lue" {
		t.Errorf("replaced: %q", v)
	}

	rec = do(t, s, "DELETE", "/api/v1/secrets/erp-password", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE: %d %s", rec.Code, rec.Body)
	}
	if ok, _ := user.Has(dir, "erp-password"); ok {
		t.Error("still set after DELETE")
	}
	rec = do(t, s, "DELETE", "/api/v1/secrets/erp-password", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("DELETE again: %d %s", rec.Code, rec.Body)
	}
	logged := log.String()
	if !strings.Contains(logged, "secret erp-password set") || !strings.Contains(logged, "secret erp-password deleted") {
		t.Errorf("server log: %s", logged)
	}
	if strings.Contains(logged, "v4lue") {
		t.Error("a value in the server log")
	}
}

func TestSecretsUnused(t *testing.T) {
	dir, s, _ := secretProject(t)
	user, _ := secretstore.User()
	at := time.Date(2026, 11, 25, 8, 0, 0, 0, time.UTC)
	for _, name := range []string{"erp-password", "old-ftp-password"} {
		if err := user.Set(dir, name, "V4lue-"+name, at); err != nil {
			t.Fatal(err)
		}
	}
	info := secretsOf(t, s)
	if len(info.Local.Unused) != 1 || info.Local.Unused[0] != (localValue{Name: "old-ftp-password", Updated: "2026-11-25T08:00:00Z"}) {
		t.Errorf("unused: %+v", info.Local.Unused)
	}
	// Read only from env in all environments: the value in the local store is unused.
	writeFile(t, filepath.Join(dir, "connections", "erp.yaml"), "kind: Connection\napiVersion: runnel/v1\nname: erp\ntype: postgres\nwith:\n  host: h\n  database: d\n  user: u\n  password: ${secret:env/erp-password}\n")
	info = secretsOf(t, s)
	if len(info.Local.Unused) != 2 {
		t.Errorf("unused: %+v", info.Local.Unused)
	}
}

func TestSetSecretRejected(t *testing.T) {
	_, s, log := secretProject(t)
	big := strings.Repeat("a", secretstore.MaxValue+1)
	cases := []struct {
		name, method, target, body string
		header                     []string
		status                     int
		code, msg                  string
	}{
		{"other origin", "PUT", "/api/v1/secrets/erp-password", `{"value":"x"}`, []string{"Origin", "http://evil.example"}, 403, "", "other origins"},
		{"other host", "PUT", "/api/v1/secrets/erp-password", `{"value":"x"}`, []string{"Host", "evil.example:7300"}, 403, "", "localhost"},
		{"other origin delete", "DELETE", "/api/v1/secrets/erp-password", "", []string{"Origin", "http://evil.example"}, 403, "", "other origins"},
		{"invalid name", "PUT", "/api/v1/secrets/a%20b", `{"value":"x"}`, nil, 400, "", "invalid secret name"},
		{"invalid name delete", "DELETE", "/api/v1/secrets/%C3%A4", "", nil, 400, "", "invalid secret name"},
		{"body too large", "PUT", "/api/v1/secrets/erp-password", `{"value":"` + strings.Repeat("a", maxSecretBody) + `"}`, nil, 413, "", "larger than 1 MB"},
		{"value too large", "PUT", "/api/v1/secrets/erp-password", `{"value":"` + big + `"}`, nil, 413, "", "larger than 1 MB"},
		{"empty value", "PUT", "/api/v1/secrets/erp-password", `{"value":""}`, nil, 400, "", "empty"},
		{"no body", "PUT", "/api/v1/secrets/erp-password", ``, nil, 400, "", "no body"},
		{"unknown field", "PUT", "/api/v1/secrets/erp-password", `{"value":"x","expires":"2027-01-01"}`, nil, 400, "", "unknown field"},
		{"unknown environment", "PUT", "/api/v1/secrets/erp-password", `{"value":"x","environment":"qa"}`, nil, 400, "", `unknown environment "qa"`},
		{"env", "PUT", "/api/v1/secrets/erp-password", `{"value":"x","environment":"test"}`, nil, 409, CodeNotLocal, "secret store env, not local"},
		{"inline", "PUT", "/api/v1/secrets/erp-password", `{"value":"x","environment":"dev"}`, nil, 409, CodeNotLocal, "secret store inline, not local"},
		{"store in the reference", "PUT", "/api/v1/secrets/erp-password", `{"value":"x","environment":"prod","store":"env"}`, nil, 409, CodeNotLocal, "secret store env"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := do(t, s, c.method, c.target, c.body, c.header...)
			e := errorBody(t, rec.Body.Bytes())
			if rec.Code != c.status || e.Code != c.code || !strings.Contains(e.Error, c.msg) {
				t.Errorf("%d %+v, want %d %q %q", rec.Code, e, c.status, c.code, c.msg)
			}
		})
	}
	user, _ := secretstore.User()
	if list, _ := user.List(s.dir); len(list) != 0 {
		t.Errorf("stored after rejections: %+v", list)
	}
	if log.String() != "" {
		t.Errorf("server log: %s", log)
	}
}

func TestSetSecretKeys(t *testing.T) {
	dir, s, log := secretProject(t)
	if runtime.GOOS != "windows" { // Windows has DPAPI instead of a missing key
		t.Setenv(secretstore.MasterKeyEnv, "")
		rec := do(t, s, "PUT", "/api/v1/secrets/erp-password", `{"value":"S3cr3t-x"}`)
		e := errorBody(t, rec.Body.Bytes())
		if rec.Code != http.StatusServiceUnavailable || e.Code != CodeSecretKey ||
			e.Error != `the value was not stored. The local secret store needs a key on this system: set RUNNEL_MASTER_KEY to 32 random bytes in base64 (e.g. from "openssl rand -base64 32"), then restart runnel serve` {
			t.Errorf("no key: %d %+v", rec.Code, e)
		}
	}
	t.Setenv(secretstore.MasterKeyEnv, base64.StdEncoding.EncodeToString([]byte("short")))
	rec := do(t, s, "PUT", "/api/v1/secrets/erp-password", `{"value":"S3cr3t-x"}`)
	e := errorBody(t, rec.Body.Bytes())
	if rec.Code != http.StatusServiceUnavailable || e.Code != CodeSecretKey || !strings.Contains(e.Error, "must be 32 bytes") {
		t.Errorf("invalid key: %d %+v", rec.Code, e)
	}
	// Values stored with one key cannot be mixed with another.
	t.Setenv(secretstore.MasterKeyEnv, testKeyA)
	if rec := do(t, s, "PUT", "/api/v1/secrets/erp-password", `{"value":"S3cr3t-x"}`); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	t.Setenv(secretstore.MasterKeyEnv, testKeyB)
	rec = do(t, s, "PUT", "/api/v1/secrets/other", `{"value":"S3cr3t-y"}`)
	e = errorBody(t, rec.Body.Bytes())
	if rec.Code != http.StatusConflict || e.Code != CodeOtherKey || !strings.Contains(e.Error, "another key") {
		t.Errorf("other key: %d %+v", rec.Code, e)
	}
	user, _ := secretstore.User()
	if ok, _ := user.Has(dir, "other"); ok {
		t.Error("stored with another key")
	}
	if strings.Contains(log.String(), "S3cr3t") {
		t.Error("a value in the server log")
	}
}

func TestSecretsOfTheService(t *testing.T) {
	_, s, _ := secretProject(t)
	service := &secretstore.Store{Dir: t.TempDir(), Machine: true}
	s.secrets = func() (*secretstore.Store, error) { return service, nil }
	for _, method := range []string{"PUT", "DELETE"} {
		rec := do(t, s, method, "/api/v1/secrets/erp-password", `{"value":"x"}`)
		e := errorBody(t, rec.Body.Bytes())
		if rec.Code != http.StatusForbidden || e.Code != CodeSecretsReadOnly || !strings.Contains(e.Error, "runnel secret set erp-password --service") {
			t.Errorf("%s: %d %+v", method, rec.Code, e)
		}
	}
	info := secretsOf(t, s)
	if info.Local.Store != "service" || info.Local.ReadOnly != "service" || info.Secrets[0].Environments["prod"].State != "missing" {
		t.Errorf("%+v", info)
	}
}

func TestSecretsUnreadable(t *testing.T) {
	dir, s, log := secretProject(t)
	// A damaged file of the project in the store: it cannot be read. (A file where the
	// folder should be is not enough: Windows reports such a path as not existing.)
	blocked := filepath.Join(t.TempDir(), "secrets")
	store := &secretstore.Store{Dir: blocked}
	if err := store.Set(dir, "other", "x", time.Now()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(blocked)
	if err != nil || len(entries) == 0 {
		t.Fatalf("store files %v %v", entries, err)
	}
	for _, e := range entries {
		if err := os.WriteFile(filepath.Join(blocked, e.Name()), []byte("not a store file"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s.secrets = func() (*secretstore.Store, error) { return store, nil }
	for range 2 {
		info := secretsOf(t, s)
		if !strings.Contains(info.Local.Error, "cannot be read; details are in the server log") || strings.Contains(info.Local.Error, blocked) ||
			info.Secrets[0].Environments["prod"].State != "unavailable" || info.Secrets[0].Environments["dev"].State != "set" {
			t.Errorf("%+v", info)
		}
	}
	if n := strings.Count(log.String(), "the local secret store cannot be read"); n != 1 {
		t.Errorf("logged %d times: %s", n, log)
	}
	rec := do(t, s, "PUT", "/api/v1/secrets/erp-password", `{"value":"S3cr3t-z"}`)
	e := errorBody(t, rec.Body.Bytes())
	if rec.Code != http.StatusInternalServerError || strings.Contains(e.Error, blocked) || !strings.Contains(e.Error, "details are in the server log") {
		t.Errorf("%d %+v", rec.Code, e)
	}
}
