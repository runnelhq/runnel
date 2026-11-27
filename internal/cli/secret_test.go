package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/secretstore"
)

func runIn(t *testing.T, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = execute(context.Background(), args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

// secretProject makes a project with a workflow that logs a secret of the local store
// (TestMain keeps the store of the user in a temporary folder).
func secretTestProject(t *testing.T) string {
	t.Helper()
	t.Setenv(secretstore.MasterKeyEnv, base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789ABCDEF")))
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: s\n")
	writeFile(t, filepath.Join(dir, "environments", "dev.yaml"), "kind: Environment\napiVersion: runnel/v1\nname: dev\n")
	writeFile(t, filepath.Join(dir, "environments", "ci.yaml"), "kind: Environment\napiVersion: runnel/v1\nname: ci\nsecretStore: env\n")
	writeFile(t, filepath.Join(dir, "workflows", "s.workflow.yaml"), `kind: Workflow
apiVersion: runnel/v1
name: s
actions:
  - id: start
    type: core.start
  - id: js
    type: core.script
    with:
      params:
        token: ${secret:local/api-token}
      script: |
        console.log("token " + params.token);
        return { token: params.token };
links:
  - { from: start, to: js }
`)
	writeFile(t, filepath.Join(dir, "connections", "api.yaml"), "kind: Connection\napiVersion: runnel/v1\nname: api\ntype: http\nwith:\n  baseUrl: https://h\n  auth: bearer\n  token: ${secret:api-token}\n")
	return dir
}

func TestSecretCommands(t *testing.T) {
	dir := secretTestProject(t)
	t.Setenv("API_TOKEN_UNSET", "")

	// Nothing set yet.
	code, out, _ := run(t, "secret", "list", dir)
	if code != 0 || !strings.Contains(out, "No secrets for the project") {
		t.Fatalf("list: %d\n%s", code, out)
	}

	// Nothing set: missing lists each use per environment and ends with 1.
	code, out, _ = run(t, "secret", "missing", dir)
	if code != 1 || !strings.Contains(out, "api-token  ci           env    missing  connections/api.yaml:8:10") ||
		!strings.Contains(out, "api-token  dev          local  missing  connections/api.yaml:8:10") ||
		!strings.Contains(out, "api-token  ci           local  missing  workflows/s.workflow.yaml:11:16") || !strings.Contains(out, "4 secrets not set.") {
		t.Fatalf("missing: %d\n%s", code, out)
	}

	// From stdin, one line break at the end is removed.
	if code, out, errOut := runIn(t, "T0ken-fr0m-stdin\r\n", "secret", "set", "api-token", dir); code != 0 || !strings.Contains(out, "Secret api-token is set for the project") || strings.Contains(out+errOut, "T0ken") {
		t.Fatalf("set: %d %q %q", code, out, errOut)
	}
	key := filepath.Join(t.TempDir(), "id_test")
	writeFile(t, key, "-----BEGIN KEY-----\nabc\n-----END KEY-----\n")
	if code, _, errOut := run(t, "secret", "set", "sftp-key", dir, "--from-file", key); code != 0 {
		t.Fatalf("from file: %d %s", code, errOut)
	}
	code, out, _ = run(t, "secret", "list", dir)
	if code != 0 || !strings.HasPrefix(out, "NAME       UPDATED\napi-token  20") || !strings.Contains(out, "\nsftp-key   20") || strings.Contains(out, "T0ken") {
		t.Errorf("list: %d\n%s", code, out)
	}
	store, _ := secretstore.User()
	if v, _, err := store.Get(dir, "sftp-key"); err != nil || v != "-----BEGIN KEY-----\nabc\n-----END KEY-----\n" {
		t.Errorf("from file: %q %v", v, err)
	}
	if v, _, _ := store.Get(dir, "api-token"); v != "T0ken-fr0m-stdin" {
		t.Errorf("stdin: %q", v)
	}

	// Only the environment with secretStore: env still misses it.
	code, out, _ = run(t, "secret", "missing", dir)
	if code != 1 || strings.Contains(out, "local") || !strings.Contains(out, "1 secret not set.") {
		t.Errorf("missing: %d\n%s", code, out)
	}
	t.Setenv("api-token", "x")
	if runtime.GOOS != "windows" { // names of environment variables with - work on Unix only
		if code, out, _ = run(t, "secret", "missing", dir); code != 0 || !strings.Contains(out, "No secrets missing (2 secrets used by the project).") {
			t.Errorf("missing: %d\n%s", code, out)
		}
	}

	// A run reads the secret from the store and masks it.
	code, out, errOut := run(t, "run", "-v", filepath.Join(dir, "workflows", "s.workflow.yaml"))
	if code != 0 || !strings.Contains(out+errOut, "token ***") || strings.Contains(out+errOut, "T0ken-fr0m-stdin") {
		t.Errorf("run: %d\n%s\n%s", code, out, errOut)
	}

	if code, out, _ := run(t, "secret", "delete", "api-token", dir); code != 0 || out != "Secret api-token is deleted.\n" {
		t.Errorf("delete: %d %q", code, out)
	}
	if code, _, errOut := run(t, "secret", "delete", "api-token", dir); code != 1 || !strings.Contains(errOut, "secret api-token is not set for the project") {
		t.Errorf("delete again: %d %q", code, errOut)
	}
	// Without the value, the run does not start; the message names the secret.
	if code, _, errOut := run(t, "run", filepath.Join(dir, "workflows", "s.workflow.yaml")); code == 0 || !strings.Contains(errOut, `secret "api-token" is not set in the local secret store of this project`) {
		t.Errorf("run without secret: %d %s", code, errOut)
	}
}

func TestSecretErrors(t *testing.T) {
	dir := secretTestProject(t)
	binary := filepath.Join(t.TempDir(), "cert.pfx")
	if err := os.WriteFile(binary, []byte{0x30, 0x82, 0xff, 0xfe, 0x00}, 0o600); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(t.TempDir(), "big")
	if err := os.WriteFile(big, bytes.Repeat([]byte("x"), secretstore.MaxValue+1), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		stdin string
		args  []string
		code  int
		want  string
	}{
		{"x", []string{"secret", "set"}, exitUsage, "accepts between 1 and 2 arg(s)"},
		{"x", []string{"secret", "set", "a b", dir}, exitUsage, `invalid secret name "a b"`},
		{"x", []string{"secret", "set", "x", dir, "extra"}, exitUsage, "accepts between 1 and 2 arg(s)"},
		{"x", []string{"secret", "list", "--bogus"}, exitUsage, "unknown flag: --bogus"},
		{"x", []string{"secret", "delete", "a/b", dir}, exitUsage, "invalid secret name"},
		{"", []string{"secret", "set", "pw", dir}, 1, "the value is empty"},
		{"\n", []string{"secret", "set", "pw", dir}, 1, "the value is empty"},
		{"", []string{"secret", "set", "pw", dir, "--from-file", binary}, 1, "not text (UTF-8); use --base64"},
		{"", []string{"secret", "set", "pw", dir, "--from-file", big}, 1, "larger than 1 MB"},
		{"x", []string{"secret", "set", "pw", t.TempDir()}, 1, "no project (project.yaml) found"},
		{"", []string{"secret", "list", t.TempDir()}, 1, "no project (project.yaml) found"},
		{"", []string{"secret", "missing", t.TempDir()}, exitUsage, "no project (project.yaml) found"},
	} {
		code, out, errOut := runIn(t, tt.stdin, tt.args...)
		if code != tt.code || !strings.Contains(errOut, tt.want) {
			t.Errorf("%v: exit code %d, stdout %q, stderr %q; want %d and %q", tt.args, code, out, errOut, tt.code, tt.want)
		}
	}
	// Binary files go in base64.
	if code, _, errOut := run(t, "secret", "set", "cert", dir, "--from-file", binary, "--base64"); code != 0 {
		t.Fatalf("base64: %d %s", code, errOut)
	}
	store, _ := secretstore.User()
	if v, _, _ := store.Get(dir, "cert"); v != base64.StdEncoding.EncodeToString([]byte{0x30, 0x82, 0xff, 0xfe, 0x00}) {
		t.Errorf("base64: %q", v)
	}
	// An invalid master key is reported without its value.
	t.Setenv(secretstore.MasterKeyEnv, "not-a-key")
	if code, _, errOut := runIn(t, "x", "secret", "set", "pw", dir); code != 1 || !strings.Contains(errOut, "RUNNEL_MASTER_KEY is not valid base64") || strings.Contains(errOut, "not-a-key") {
		t.Errorf("master key: %d %s", code, errOut)
	}
	if runtime.GOOS != "windows" {
		// Without it, systems without DPAPI say how to set one.
		t.Setenv(secretstore.MasterKeyEnv, "")
		if code, _, errOut := runIn(t, "x", "secret", "set", "pw", dir); code != 1 || !strings.Contains(errOut, "set RUNNEL_MASTER_KEY to 32 random bytes in base64") {
			t.Errorf("no master key: %d %s", code, errOut)
		}
		if code, _, errOut := run(t, "secret", "list", dir, "--service"); code != 1 || !strings.Contains(errOut, "exists only on Windows") {
			t.Errorf("service: %d %s", code, errOut)
		}
	}
}
