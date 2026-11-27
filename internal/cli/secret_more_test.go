package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/secretstore"
)

// "runnel secret missing" reads only the YAML files of the project: hidden folders,
// other files, broken YAML and links are left out; several uses of a secret are counted
// at the first. A project without environments has one column "-".
func TestSecretMissingFiles(t *testing.T) {
	secretTestProject(t) // sets the master key
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: m\n")
	conn := "kind: Connection\napiVersion: runnel/v1\nname: %s\ntype: http\nwith:\n  baseUrl: https://h\n  auth: bearer\n  token: ${secret:%s}\n"
	writeFile(t, filepath.Join(dir, "connections", "a.yaml"), strings.ReplaceAll(strings.Replace(conn, "%s", "a", 1), "%s", "shared"))
	writeFile(t, filepath.Join(dir, "connections", "b.YML"), strings.ReplaceAll(strings.Replace(conn, "%s", "b", 1), "%s", "shared"))
	writeFile(t, filepath.Join(dir, ".git", "c.yaml"), strings.ReplaceAll(strings.Replace(conn, "%s", "c", 1), "%s", "hidden"))
	writeFile(t, filepath.Join(dir, "data", "notes.txt"), "${secret:text}\n")
	writeFile(t, filepath.Join(dir, "data", "broken.yaml"), "kind: [\n${secret:broken}\n")
	if runtime.GOOS != "windows" { // links need extra rights on Windows
		outside := t.TempDir()
		writeFile(t, filepath.Join(outside, "x.yaml"), strings.ReplaceAll(strings.Replace(conn, "%s", "x", 1), "%s", "linked"))
		if err := os.Symlink(outside, filepath.Join(dir, "linked")); err != nil {
			t.Fatal(err)
		}
	}
	code, out, errOut := run(t, "secret", "missing", dir)
	if code != 1 {
		t.Fatalf("exit code %d, stderr %s", code, errOut)
	}
	want := "NAME    ENVIRONMENT  STORE  STATE    USED IN\n" +
		"shared  -            local  missing  connections/a.yaml:8:10 (+1)\n" +
		"1 secret not set.\n"
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}

	if code, out, _ := runIn(t, "v", "secret", "set", "shared", dir); code != 0 {
		t.Fatalf("set: %d %s", code, out)
	}
	if code, out, _ := run(t, "secret", "missing", dir); code != 0 || out != "No secrets missing (1 secret used by the project).\n" {
		t.Errorf("all set: %d %q", code, out)
	}
}

// A project that cannot be read ends "runnel secret missing" with 2, not 1 (a secret
// missing).
func TestSecretMissingBrokenProject(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: [\n")
	if code, out, errOut := run(t, "secret", "missing", dir); code != exitUsage || out != "" || !strings.Contains(errOut, "project.yaml") {
		t.Errorf("exit code %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// Further refusals of "runnel secret set": a file that does not exist, a value on stdin
// beyond the limit, and the store of the service on other systems than Windows.
func TestSecretSetRefusals(t *testing.T) {
	dir := secretTestProject(t)
	type refusal struct {
		stdin string
		args  []string
		want  string
	}
	tests := []refusal{
		{"", []string{"secret", "set", "pw", dir, "--from-file", filepath.Join(t.TempDir(), "none")}, "none"},
		{strings.Repeat("x", secretstore.MaxValue+1), []string{"secret", "set", "pw", dir}, "the value is larger than 1 MB"},
		{"x\x00\xff", []string{"secret", "set", "pw", dir}, "not text (UTF-8)"},
	}
	if runtime.GOOS != "windows" {
		tests = append(tests,
			refusal{"x", []string{"secret", "set", "pw", dir, "--service"}, "exists only on Windows"},
			refusal{"", []string{"secret", "delete", "pw", dir, "--service"}, "exists only on Windows"},
			refusal{"", []string{"secret", "missing", dir, "--service"}, "exists only on Windows"},
		)
	}
	for _, tt := range tests {
		code, out, errOut := runIn(t, tt.stdin, tt.args...)
		if code == 0 || out != "" || !strings.Contains(errOut, tt.want) {
			t.Errorf("%v: exit code %d, stdout %q, stderr %q; want %q", tt.args, code, out, errOut, tt.want)
		}
	}
	store, _ := secretstore.User()
	if list, err := store.List(dir); err != nil || len(list) != 0 {
		t.Errorf("stored after refusals: %v %v", list, err)
	}
}

// In the project folder, messages name the project by its absolute path instead of ".";
// a file given as stdin is no terminal and is read as it is.
func TestSecretInProjectFolder(t *testing.T) {
	dir := secretTestProject(t)
	t.Chdir(dir)
	abs, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if code, out, _ := run(t, "secret", "list"); code != 0 || out != "No secrets for the project "+abs+" in the local store of the user.\n" {
		t.Errorf("list: %d %q", code, out)
	}
	if code, _, errOut := run(t, "secret", "delete", "pw"); code != 1 || !strings.Contains(errOut, "secret pw is not set for the project "+abs+" (local store of the user)") {
		t.Errorf("delete: %d %q", code, errOut)
	}
	if isTerminal(strings.NewReader("x")) {
		t.Error("a reader is no terminal")
	}
	f, err := os.Open(filepath.Join(dir, "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if isTerminal(f) {
		t.Error("a file is no terminal")
	}
}
