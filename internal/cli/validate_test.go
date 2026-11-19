package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

// The tests run from the repository root, so that paths in the output match
// what a user sees when running "runnel validate examples/...".
func chdirRepoRoot(t *testing.T) {
	t.Helper()
	t.Chdir(filepath.Join("..", ".."))
}

func TestValidateExamples(t *testing.T) {
	golden, err := filepath.Abs(filepath.Join("testdata", "validate-examples.golden"))
	if err != nil {
		t.Fatal(err)
	}
	chdirRepoRoot(t)

	code, out, errOut := run(t, "validate", "examples/hello")
	if code != 0 || errOut != "" {
		t.Fatalf("examples/hello: exit code %d, stderr %q, stdout:\n%s", code, errOut, out)
	}
	if want := "3 workflow files checked: 0 errors, 0 warnings\n"; out != want {
		t.Errorf("examples/hello output = %q, want %q", out, want)
	}

	code, out, _ = run(t, "validate", "examples")
	if code != 1 {
		t.Errorf("examples: exit code %d, want 1", code)
	}
	if *update {
		if err := os.WriteFile(golden, []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("missing golden file (run with -update): %v", err)
	}
	if !bytes.Equal([]byte(out), want) {
		t.Errorf("output differs from %s (run with -update if intended)\n--- got:\n%s\n--- want:\n%s", golden, out, want)
	}
}

func TestValidateStrict(t *testing.T) {
	chdirRepoRoot(t)
	file := "internal/model/testdata/workflow/graph/unreachable.workflow.yaml" // warnings only

	if code, out, _ := run(t, "validate", file); code != 0 || !strings.Contains(out, "0 errors, 3 warnings") {
		t.Errorf("without --strict: exit code %d, output:\n%s", code, out)
	}
	if code, _, _ := run(t, "validate", "--strict", file); code != 1 {
		t.Errorf("with --strict: exit code %d, want 1", code)
	}
}

func TestValidateNoFiles(t *testing.T) {
	code, _, errOut := run(t, "validate", t.TempDir())
	if code != 1 || !strings.Contains(errOut, "no workflow files") {
		t.Errorf("exit code %d, stderr %q", code, errOut)
	}

	code, _, errOut = run(t, "validate", filepath.Join(t.TempDir(), "missing.workflow.yaml"))
	if code != 1 || !strings.Contains(errOut, "Error:") {
		t.Errorf("missing file: exit code %d, stderr %q", code, errOut)
	}
}

func TestFindWorkflowFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"a.workflow.yaml",
		"sub/b.workflow.yaml",
		"sub/notes.yaml",
		".hidden/c.workflow.yaml",
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	explicit := filepath.Join(dir, "sub", "notes.yaml")

	got, err := findWorkflowFiles([]string{dir, explicit, filepath.Join(dir, "a.workflow.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(dir, "a.workflow.yaml"),
		filepath.Join(dir, "sub", "b.workflow.yaml"),
		explicit,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestValidateProject(t *testing.T) {
	golden, err := filepath.Abs(filepath.Join("testdata", "validate-project.golden"))
	if err != nil {
		t.Fatal(err)
	}
	chdirRepoRoot(t)
	const okProject = "internal/model/testdata/project/ok"

	var all strings.Builder
	for _, args := range [][]string{
		{"validate", okProject},                                                 // all environments
		{"validate", "--env", "dev", okProject},                                 // only dev: API_URL is defined
		{"validate", "--env", "dev", "--param", "UNDEFINED_USER=me", okProject}, // parameter defines the variable
		{"validate", "internal/model/testdata/project/invalid"},                 // errors in project and environments
	} {
		code, out, errOut := run(t, args...)
		all.WriteString("$ runnel " + strings.Join(args, " ") + "\n" + out + errOut)
		all.WriteString("exit code " + strconv.Itoa(code) + "\n\n")
	}
	compareGoldenFile(t, golden, all.String())
}

func TestValidateFlagErrors(t *testing.T) {
	chdirRepoRoot(t)
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"validate", "--env", "qa", "examples/hello"}, `environment "qa" not found in project "hello" (available: dev, prod)`},
		{[]string{"validate", "--env", "dev", "examples/broken/graph.workflow.yaml"}, "no project (project.yaml) found"},
		{[]string{"validate", "--param", "no-equals", "examples/hello"}, `invalid --param "no-equals"`},
		{[]string{"validate", "--param", "1X=y", "examples/hello"}, `invalid --param "1X=y"`},
	} {
		code, _, errOut := run(t, tt.args...)
		if code != 1 || !strings.Contains(errOut, tt.want) {
			t.Errorf("%v: exit code %d, stderr %q, want containing %q", tt.args, code, errOut, tt.want)
		}
	}
}

func compareGoldenFile(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden file (run with -update): %v", err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s (run with -update if intended)\n--- got:\n%s\n--- want:\n%s", path, got, want)
	}
}
