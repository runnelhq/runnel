package secretstore

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testKey is a fixed master key for tests (never use it elsewhere).
var testKey = base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789ABCDEF"))

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	t.Setenv(MasterKeyEnv, testKey)
	return &Store{Dir: filepath.Join(t.TempDir(), "secrets")}, t.TempDir()
}

var now = time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

func TestRoundTrip(t *testing.T) {
	s, project := newStore(t)
	if _, ok, err := s.Get(project, "erp-password"); ok || err != nil {
		t.Fatalf("empty store: %v %v", ok, err)
	}
	value := "Pa55word!\nline 2 ✓"
	if err := s.Set(project, "erp-password", value, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(project, "api.token", "t-123", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := s.Get(project, "erp-password"); err != nil || !ok || v != value {
		t.Fatalf("get: %q %v %v", v, ok, err)
	}
	list, err := s.List(project)
	if err != nil || len(list) != 2 || list[0].Name != "api.token" || list[1].Name != "erp-password" || !list[1].Updated.Equal(now) {
		t.Fatalf("list: %v %v", list, err)
	}
	if ok, err := s.Has(project, "api.token"); !ok || err != nil {
		t.Errorf("has: %v %v", ok, err)
	}
	// The file holds no value in plain text and lies outside the project.
	files, _ := filepath.Glob(filepath.Join(s.Dir, "*.json"))
	if len(files) != 1 {
		t.Fatalf("files %v", files)
	}
	data, _ := os.ReadFile(files[0])
	if bytes.Contains(data, []byte("Pa55word")) || bytes.Contains(data, []byte("t-123")) {
		t.Errorf("plain text in %s", data)
	}
	if info, _ := os.Stat(files[0]); info.Mode().Perm()&0o077 != 0 && !isWindows() {
		t.Errorf("permissions %v", info.Mode())
	}
	// Another project does not see the secrets.
	if _, ok, _ := s.Get(t.TempDir(), "erp-password"); ok {
		t.Error("secret of another project")
	}
	if ok, err := s.Delete(project, "erp-password"); !ok || err != nil {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if ok, err := s.Delete(project, "erp-password"); ok || err != nil {
		t.Fatalf("delete again: %v %v", ok, err)
	}
	if _, ok, _ := s.Get(project, "erp-password"); ok {
		t.Error("deleted secret still set")
	}
	// No temporary files are left.
	if tmp, _ := filepath.Glob(filepath.Join(s.Dir, ".*")); len(tmp) != 0 {
		t.Errorf("temporary files %v", tmp)
	}
}

func isWindows() bool { return filepath.Separator == '\\' }

func TestWrongKey(t *testing.T) {
	s, project := newStore(t)
	if err := s.Set(project, "pw", "Pa55word!", now); err != nil {
		t.Fatal(err)
	}
	t.Setenv(MasterKeyEnv, base64.StdEncoding.EncodeToString([]byte("another key of 32 bytes ........")))
	if _, _, err := s.Get(project, "pw"); err == nil || !strings.Contains(err.Error(), "stored with another key") {
		t.Errorf("get: %v", err)
	}
	if err := s.Set(project, "other", "x", now); err == nil || !strings.Contains(err.Error(), "stored with another key") {
		t.Errorf("set: %v", err)
	}
	// Names and deleting need no key.
	if list, err := s.List(project); err != nil || len(list) != 1 {
		t.Errorf("list: %v %v", list, err)
	}
}

func readFile(t *testing.T, s *Store) (string, *file) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(s.Dir, "*.json"))
	if len(files) != 1 {
		t.Fatalf("files %v", files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	f, err := decodeFile(data)
	if err != nil {
		t.Fatal(err)
	}
	return files[0], f
}

func writeFile(t *testing.T, path string, f *file) {
	t.Helper()
	data, _ := json.Marshal(f)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A changed value, or values swapped between names or projects, do not decrypt.
func TestTampered(t *testing.T) {
	s, project := newStore(t)
	for _, n := range []string{"a-secret", "b-secret"} {
		if err := s.Set(project, n, "value of "+n, now); err != nil {
			t.Fatal(err)
		}
	}
	path, f := readFile(t, s)
	orig, _ := os.ReadFile(path)

	e := f.Secrets["a-secret"]
	e.Value[0] ^= 1
	f.Secrets["a-secret"] = e
	writeFile(t, path, f)
	if _, _, err := s.Get(project, "a-secret"); err == nil || !strings.Contains(err.Error(), "cannot be decrypted") || strings.Contains(err.Error(), "value of") {
		t.Errorf("tampered: %v", err)
	}

	// Swapped names.
	_ = os.WriteFile(path, orig, 0o600)
	_, f = readFile(t, s)
	f.Secrets["a-secret"], f.Secrets["b-secret"] = f.Secrets["b-secret"], f.Secrets["a-secret"]
	writeFile(t, path, f)
	if _, _, err := s.Get(project, "a-secret"); err == nil || !strings.Contains(err.Error(), "cannot be decrypted") {
		t.Errorf("swapped names: %v", err)
	}

	// The file of another project, copied over.
	_ = os.WriteFile(path, orig, 0o600)
	other := t.TempDir()
	otherKey, _ := projectKey(other)
	_, f = readFile(t, s)
	f.Project = otherKey
	writeFile(t, s.path(otherKey), f)
	if _, _, err := s.Get(other, "a-secret"); err == nil || !strings.Contains(err.Error(), "cannot be decrypted") {
		t.Errorf("other project: %v", err)
	}
	// A file under the name of another project is refused.
	writeFile(t, path, f)
	if _, _, err := s.Get(project, "a-secret"); err == nil || !strings.Contains(err.Error(), "belongs to the project") {
		t.Errorf("wrong project: %v", err)
	}
}

func TestDecodeFileRejects(t *testing.T) {
	for in, want := range map[string]string{
		`not json`:                     "not a file of the local secret store",
		`{"version":2,"project":"/p"}`: "version 2",
		`{"version":1}`:                "no project",
		`{"version":1,"project":"/p","secrets":{"a b":{"nonce":"AAAAAAAAAAAAAAAA","value":"AAAAAAAAAAAAAAAAAAAAAA=="}}}`: "invalid secret name",
		`{"version":1,"project":"/p","secrets":{"ab":{"nonce":"AAAA","value":"AAAAAAAAAAAAAAAAAAAAAA=="}}}`:              "damaged",
	} {
		if _, err := decodeFile([]byte(in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", in, err, want)
		}
	}
}

func TestParseMasterKey(t *testing.T) {
	good := []byte("0123456789abcdef0123456789ABCDEF")
	for _, in := range []string{
		base64.StdEncoding.EncodeToString(good),
		base64.RawURLEncoding.EncodeToString(good),
		" " + base64.StdEncoding.EncodeToString(good) + "\n",
	} {
		if k, err := ParseMasterKey(in); err != nil || !bytes.Equal(k, good) {
			t.Errorf("%q: %v", in, err)
		}
	}
	for in, want := range map[string]string{
		"correct horse battery staple":                      "not valid base64",
		base64.StdEncoding.EncodeToString([]byte("short")):  "32 bytes in base64, not 5",
		base64.StdEncoding.EncodeToString(make([]byte, 32)): "not random",
		base64.StdEncoding.EncodeToString(make([]byte, 64)): "not 64",
	} {
		_, err := ParseMasterKey(in)
		if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), in) {
			t.Errorf("%q: %v, want %q", in, err, want)
		}
	}
	// An invalid key in the environment stops the store before anything is written.
	s, project := newStore(t)
	t.Setenv(MasterKeyEnv, "secret-but-not-base64!")
	if err := s.Set(project, "pw", "x", now); err == nil || !strings.Contains(err.Error(), "not valid base64") || strings.Contains(err.Error(), "secret-but") {
		t.Errorf("set: %v", err)
	}
}

func TestSetRejects(t *testing.T) {
	s, project := newStore(t)
	if err := s.Set(project, "a b", "x", now); err == nil || !strings.Contains(err.Error(), "invalid secret name") {
		t.Errorf("name: %v", err)
	}
	if err := s.Set(project, "big", strings.Repeat("x", MaxValue+1), now); err == nil || !strings.Contains(err.Error(), "larger than 1 MB") {
		t.Errorf("size: %v", err)
	}
}

// Without RUNNEL_MASTER_KEY, systems without DPAPI report how to set a key.
func TestNoKey(t *testing.T) {
	if dpapiAvailable {
		t.Skip("DPAPI is available")
	}
	s, project := newStore(t)
	t.Setenv(MasterKeyEnv, "")
	if err := s.Set(project, "pw", "x", now); err == nil || !strings.Contains(err.Error(), "set RUNNEL_MASTER_KEY") {
		t.Errorf("set: %v", err)
	}
}

func TestDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	u, err := Default()
	if err != nil || !strings.HasSuffix(u.Dir, filepath.Join("runnel", "secrets")) || u.Machine {
		t.Fatalf("%+v %v", u, err)
	}
	svc := &Store{Dir: t.TempDir(), Machine: true}
	SetDefault(svc)
	defer SetDefault(nil)
	if d, _ := Default(); d != svc {
		t.Errorf("default %+v", d)
	}
}

// TestMasterKeyBeforeDPAPI: RUNNEL_MASTER_KEY wins over DPAPI for the store of the user and
// of the service, so no key file is made (on Windows too).
func TestMasterKeyBeforeDPAPI(t *testing.T) {
	t.Setenv(MasterKeyEnv, testKey)
	project := t.TempDir()
	for _, machine := range []bool{false, true} {
		s := &Store{Dir: filepath.Join(t.TempDir(), "secrets"), Machine: machine}
		if err := s.Set(project, "pw", "Pa55word!", now); err != nil {
			t.Fatal(err)
		}
		if v, ok, err := s.Get(project, "pw"); err != nil || !ok || v != "Pa55word!" {
			t.Fatalf("machine=%v: Get = %q, %v, %v", machine, v, ok, err)
		}
		if _, err := os.Stat(filepath.Join(s.Dir, keyFile)); !os.IsNotExist(err) {
			t.Fatalf("machine=%v: key file made although %s is set: %v", machine, MasterKeyEnv, err)
		}
	}
}
