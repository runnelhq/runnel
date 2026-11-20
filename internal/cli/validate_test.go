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
	file := writeWorkflow(t, `
actions:
  - { id: start, type: core.start }
  - { id: used, type: core.log, with: { message: "${UNDEFINED}" } }
  - { id: orphan, type: core.dummy }
links:
  - { from: start, to: used }
`) // warnings only

	if code, out, _ := run(t, "validate", file); code != 0 || !strings.Contains(out, "0 errors, 2 warnings") {
		t.Errorf("without --strict: exit code %d, output:\n%s", code, out)
	}
	if code, _, _ := run(t, "validate", "--strict", file); code != 1 {
		t.Errorf("with --strict: exit code %d, want 1", code)
	}
}

// validate reports what a run would reject: unknown action types and invalid configurations.
func TestValidateActionTypesAndConfig(t *testing.T) {
	file := writeWorkflow(t, `
actions:
  - { id: start, type: core.start }
  - { id: copy, type: file.shred }
  - { id: greet, type: core.log, with: { msg: hi } }
links:
  - { from: start, to: copy }
  - { from: copy, to: greet }
`)
	code, out, _ := run(t, "validate", file)
	for _, want := range []string{
		`:7:23: error: action "copy": unknown action type "file.shred"`,
		`:8:42: error: action "greet" (core.log): unknown field "msg"`,
		"2 errors, 0 warnings",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if code != 1 {
		t.Errorf("exit code %d, want 1", code)
	}
}

func TestValidateNoFiles(t *testing.T) {
	code, _, errOut := run(t, "validate", t.TempDir())
	if code != 1 || !strings.Contains(errOut, "no workflow or pipeline files (*.workflow.yaml, *.pipeline.yaml)") {
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

	got, err := findFiles([]string{dir, explicit, filepath.Join(dir, "a.workflow.yaml")})
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

func TestValidatePipelines(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("kind: Pipeline\napiVersion: runnel/v1\nname: p\n"+body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write("ok.pipeline.yaml", "transforms:\n  - { id: gen, type: core.generate-rows, with: { rows: 10 } }\n  - { id: log, type: core.log }\nlinks:\n  - { from: gen, to: log }\n")
	code, out, errOut := run(t, "validate", dir)
	if code != 0 || out != "1 pipeline file checked: 0 errors, 0 warnings\n" {
		t.Errorf("valid: exit code %d, stdout %q, stderr %q", code, out, errOut)
	}

	bad := write("bad.pipeline.yaml", "transforms:\n  - { id: read, type: csv.input, copies: 2 }\n")
	code, out, _ = run(t, "validate", bad)
	if code != 1 || !strings.Contains(out, `bad.pipeline.yaml:5:34: error: unknown field "copies"`) {
		t.Errorf("invalid: exit code %d, stdout %q", code, out)
	}
	// Types and configurations of the transforms are checked like before a run (S13b).
	types := write("types.pipeline.yaml", "transforms:\n  - { id: read, type: xml.input }\n  - { id: log, type: core.log, with: { level: loud } }\nlinks:\n  - { from: read, to: log }\n")
	_, out, _ = run(t, "validate", types)
	for _, want := range []string{
		`types.pipeline.yaml:5:23: error: transform "read": unknown transform type "xml.input"`,
		`types.pipeline.yaml:6:40: error: transform "log" (core.log): invalid level "loud"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	_ = os.Remove(types)

	// Workflows and pipelines together; the summary names both.
	wf := filepath.Join(dir, "w.workflow.yaml")
	if err := os.WriteFile(wf, []byte("kind: Workflow\napiVersion: runnel/v1\nname: w\nactions:\n  - { id: start, type: core.start }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, out, _ = run(t, "validate", dir)
	if !strings.Contains(out, "1 workflow file and 2 pipeline files checked: 1 error, 0 warnings") {
		t.Errorf("mixed: %q", out)
	}
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
