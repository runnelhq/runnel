package runs

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/actions"
)

const shellWorkflow = `kind: Workflow
apiVersion: runnel/v1
name: tool
parameters:
  - name: FILE
    default: a.txt
actions:
  - id: start
    type: core.start
  - id: run
    type: shell.run
    with:
      program: PROGRAM
      args: [ ARGS ]
      env:
        TOKEN: ${secret:TOKEN}
links:
  - { from: start, to: run }
`

// The program of shell.run takes variables of the project, but no parameters or
// expressions; secrets only in env, not in args (ADR 0023).
func TestShellRunChecks(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: t\nsecretStore: env\nvariables:\n  TOOLS: C:/Tools\n")
	t.Setenv("TOKEN", "x")
	wf := func(program, args string) string {
		return strings.NewReplacer("PROGRAM", program, "ARGS", args).Replace(shellWorkflow)
	}
	if _, err := Prepare(Spec{Name: "workflows/tool.workflow.yaml", Data: []byte(wf("${TOOLS}/tool.exe", `"${FILE}", "{{ params.FILE }}"`)), ProjectDir: dir, Registry: actions.Builtin()}); err != nil {
		t.Errorf("variables in program, parameters and expressions in args: %v", err)
	}
	for _, tt := range []struct{ program, args, want string }{
		{"${FILE}", `"x"`, "the program must not use the parameter ${FILE}"},
		{`"{{ params.FILE }}"`, `"x"`, "the program must not contain {{ … }}"},
		{"tool", `"${secret:TOKEN}"`, "${secret:TOKEN} is not allowed here"},
	} {
		_, err := Prepare(Spec{Name: "workflows/tool.workflow.yaml", Data: []byte(wf(tt.program, tt.args)), ProjectDir: dir, Registry: actions.Builtin()})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s %s: %v\nwant %q", tt.program, tt.args, err, tt.want)
		}
	}
}
