package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/scaffold"
)

// TestInit: the new project validates without findings and its example workflow runs.
func TestInit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo")
	code, out, errOut := run(t, "init", dir)
	if code != 0 || errOut != "" {
		t.Fatalf("init: exit code %d, stderr %q", code, errOut)
	}
	for _, want := range []string{`Created project "demo" in ` + dir, "  project.yaml\n", "  .gitignore\n",
		"Next steps:\n  runnel validate " + dir + "\n"} {
		if !strings.Contains(out, filepath.FromSlash(want)) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	code, out, errOut = run(t, "validate", "--strict", dir)
	if code != 0 || errOut != "" {
		t.Fatalf("validate: exit code %d, stderr %q, stdout:\n%s", code, errOut, out)
	}
	if want := "1 workflow file checked: 0 errors, 0 warnings\n"; out != want {
		t.Errorf("validate output = %q, want %q", out, want)
	}

	code, out, errOut = run(t, "run", "--history", "none", filepath.Join(dir, filepath.FromSlash(scaffold.ExampleWorkflow)))
	if code != 0 {
		t.Fatalf("run: exit code %d, stderr %q, stdout:\n%s", code, errOut, out)
	}
	if want := "Hello, World! (development)"; !strings.Contains(out+errOut, want) {
		t.Errorf("run output lacks %q:\n%s%s", want, out, errOut)
	}
}

func TestInitRefusals(t *testing.T) {
	dir := t.TempDir()
	notes := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notes, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"init", dir}, "is not empty; use an empty folder, or --force"},
		{[]string{"init", filepath.Join(dir, "new"), "--name", "my project"}, `invalid project name "my project"`},
		{[]string{"init", notes}, "not a folder"},
		{[]string{"init", "a", "b"}, "accepts at most 1 arg"},
	} {
		code, out, errOut := run(t, tt.args...)
		if code != exitUsage || out != "" || !strings.Contains(errOut, tt.want) {
			t.Errorf("%v: exit code %d, stdout %q, stderr %q; want %d and %q", tt.args, code, out, errOut, exitUsage, tt.want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "new")); err == nil {
		t.Error("folder created for an invalid name")
	}
}

// TestInitForce: --force adds the missing files and keeps the existing ones.
func TestInitForce(t *testing.T) {
	dir := t.TempDir()
	workflow := filepath.Join(dir, filepath.FromSlash(scaffold.ExampleWorkflow))
	if err := os.MkdirAll(filepath.Dir(workflow), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflow, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run(t, "init", "--force", "--name", "demo", dir)
	if code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	if want := "Kept (already there):\n  " + filepath.FromSlash(scaffold.ExampleWorkflow) + "\n"; !strings.Contains(out, want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
	if got, _ := os.ReadFile(workflow); string(got) != "mine" {
		t.Errorf("workflow overwritten: %q", got)
	}

	code, out, _ = run(t, "init", "--force", dir)
	if code != 0 || !strings.HasPrefix(out, "Nothing to add: the project files are already in "+dir) {
		t.Errorf("second run: exit code %d, stdout:\n%s", code, out)
	}
}
