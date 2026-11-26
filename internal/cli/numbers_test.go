package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Number fields (R5b2): a literal that is not a number is an error of validate with the
// position of the step; a variable is checked when the run starts.

func writePipeline(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.pipeline.yaml")
	if err := os.WriteFile(path, []byte("kind: Pipeline\napiVersion: runnel/v1\nname: test\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const numbersPipeline = `parameters:
  - { name: EVERY, default: "2" }
transforms:
  - { id: rows, type: core.generate-rows, with: { count: 4 } }
  - id: show
    type: core.log
    with:
      every: %s
links:
  - { from: rows, to: show }
`

func TestNumberFieldsPipeline(t *testing.T) {
	file := writePipeline(t, strings.Replace(numbersPipeline, "%s", "${EVERY}", 1))
	if code, out, errOut := run(t, "validate", file); code != 0 || !strings.Contains(out, "0 errors, 0 warnings") {
		t.Errorf("validate with a variable: exit code %d\n%s%s", code, out, errOut)
	}
	if code, out, errOut := run(t, "run", file); code != 0 || !strings.Contains(out, "[show] read 4, written 4") {
		t.Errorf("run: exit code %d\n%s%s", code, out, errOut)
	}
	// A value of the variable that is not a number: the run does not start.
	code, out, errOut := run(t, "run", "--param", "EVERY=often", file)
	want := `test.pipeline.yaml:11:7: error: transform "show" (core.log): field "every": expected a whole number, got "often"`
	if code != 2 || !strings.Contains(errOut, want) {
		t.Errorf("run with EVERY=often: exit code %d, want 2 and %q\n%s%s", code, want, out, errOut)
	}

	file = writePipeline(t, strings.Replace(numbersPipeline, "%s", "often", 1))
	code, out, _ = run(t, "validate", file)
	if code != 1 || !strings.Contains(out, want) {
		t.Errorf("validate with a literal: exit code %d, want 1 and %q\n%s", code, want, out)
	}
}

const numbersWorkflow = `parameters:
  - { name: MAX, default: "10" }
actions:
  - { id: start, type: core.start }
  - id: unpack
    type: archive.extract
    with:
      source: missing.zip
      target: out
      maxFiles: %s
links:
  - { from: start, to: unpack }
`

func TestNumberFieldsWorkflow(t *testing.T) {
	file := writeWorkflow(t, strings.Replace(numbersWorkflow, "%s", "${MAX}", 1))
	if code, out, errOut := run(t, "validate", file); code != 0 || !strings.Contains(out, "0 errors, 0 warnings") {
		t.Errorf("validate with a variable: exit code %d\n%s%s", code, out, errOut)
	}
	// Resolved when the action runs: a value that is not a number fails the action.
	code, out, errOut := run(t, "run", "--param", "MAX=lots", file)
	if code != 1 || !strings.Contains(out, `field "maxFiles": expected a whole number, got "lots"`) {
		t.Errorf("run with MAX=lots: exit code %d\n%s%s", code, out, errOut)
	}
	// A number as the value gets as far as the archive, which is missing.
	if code, out, _ := run(t, "run", file); code != 1 || strings.Contains(out, `field "maxFiles"`) || !strings.Contains(out, "missing.zip") {
		t.Errorf("run: exit code %d\n%s", code, out)
	}

	file = writeWorkflow(t, strings.Replace(numbersWorkflow, "%s", "lots", 1))
	code, out, _ = run(t, "validate", file)
	if want := `test.workflow.yaml:11:7: error: action "unpack" (archive.extract): field "maxFiles": expected a whole number, got "lots"`; code != 1 || !strings.Contains(out, want) {
		t.Errorf("validate with a literal: exit code %d, want 1 and %q\n%s", code, want, out)
	}
}
