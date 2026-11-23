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

// The script of shell.powershell is passed as written: ${…} and {{ … }} in it stop the
// run; values come as params, which may use both (ADR 0023).
func TestShellPowerShellChecks(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: t\n")
	wf := func(script string) string {
		return `kind: Workflow
apiVersion: runnel/v1
name: ps
parameters:
  - name: FOLDER
    default: data
actions:
  - id: start
    type: core.start
  - id: run
    type: shell.powershell
    with:
      script: "` + script + `"
      params:
        Folder: ${FOLDER}
        Upper: "{{ upper(params.FOLDER) }}"
links:
  - { from: start, to: run }
`
	}
	if _, err := Prepare(Spec{Name: "workflows/ps.workflow.yaml", Data: []byte(wf("param($Folder, $Upper) Get-ChildItem $Folder")), ProjectDir: dir, Registry: actions.Builtin()}); err != nil {
		t.Errorf("values in params: %v", err)
	}
	for _, script := range []string{"Get-ChildItem ${FOLDER}", "Get-ChildItem {{ params.FOLDER }}"} {
		_, err := Prepare(Spec{Name: "workflows/ps.workflow.yaml", Data: []byte(wf(script)), ProjectDir: dir, Registry: actions.Builtin()})
		if err == nil || !strings.Contains(err.Error(), "the script must not contain ${…} or {{ … }}") {
			t.Errorf("%s: %v", script, err)
		}
	}
}
