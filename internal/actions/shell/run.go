package shell

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/proc"
)

// shell.run starts a program with a list of arguments, without a shell in between (ADR
// 0023): values reach it only as single arguments or environment variables. Output:
// exitCode, stdout, stderr, truncated, durationMs and, with format: json, the stdout read
// as JSON.
var runDefinition = action.Definition{
	Type:        "shell.run",
	Category:    action.CategoryExecution,
	Label:       "Run Program",
	Description: "Starts a program with arguments, without a shell; output: exitCode, stdout and stderr",
	Example:     "program: robocopy\nargs: [ data/out, \"\\\\\\\\server\\\\share\\\\in\", \"*.csv\", /MIR ]\nokCodes: [ 0, 1, 2, 3 ]\ntimeout: 10m\n",
	Config:      runAction{},
	New:         newRun,
	Reference: action.Reference{
		Outputs: []action.OutputField{
			{Name: "output.exitCode", Type: "integer", Description: "Exit code of the program"},
			{Name: "output.stdout", Type: "string", Description: "Standard output, decoded; values of env are hidden"},
			{Name: "output.stderr", Type: "string", Description: "Error output, decoded; values of env are hidden"},
			{Name: "output.truncated", Type: "boolean", Description: "true if stdout or stderr was longer than maxOutput"},
			{Name: "output.durationMs", Type: "integer", Description: "Running time in milliseconds"},
			{Name: "output.value", Type: "any", Description: "With format: json, the last JSON value of stdout"},
		},
		Errors: "Fails when the program is not found, its exit code is not in okCodes (default 0; the message shows the last lines of stderr), it runs longer than timeout (it is ended with all processes it started), or with format: json stdout holds no JSON. Arguments of .bat and .cmd files with characters that cmd.exe interprets are refused.",
		Limits: []string{
			"timeout limits the program (default 10m).",
			"maxOutput limits stdout and stderr each (default 1MB, at most 1GB); more is dropped.",
		},
	},
}

type runAction struct {
	Program   string            `json:"program" doc:"Program to start: a name found through PATH, or a path relative to the project folder; may use variables of the project, but no parameters of the workflow and no expressions" schema:"required,format=runnel-program,example=robocopy"`
	Args      []string          `json:"args" doc:"Arguments, one per entry; each is passed as one argument, never read by a shell, and may use ${…} and {{ … }}"`
	OKCodes   []int             `json:"okCodes" doc:"Exit codes that count as success; empty: 0 only"`
	Workdir   string            `json:"workdir" doc:"Working folder, relative to the project folder; empty: the project folder"`
	Env       map[string]string `json:"env" doc:"Further environment variables; the only place for secrets (${secret:…}), as arguments are visible to other users" schema:"format=runnel-secret"`
	Input     *string           `json:"input" doc:"Text for stdin; missing: stdin is empty, so that a program waiting for input does not hang"`
	Encoding  string            `json:"encoding" doc:"Encoding of stdout and stderr; auto: UTF-8, else the OEM code page of Windows" schema:"enum=auto|utf-8|oem|windows-1252,default=auto"`
	Format    string            `json:"format" doc:"text, or json to read the last JSON value of stdout into output.value" schema:"enum=text|json,default=text"`
	MaxOutput string            `json:"maxOutput" doc:"Largest size of stdout and of stderr kept, e.g. 500KB or 10MB; more is dropped (truncated)" schema:"default=1MB"`
	Timeout   string            `json:"timeout" doc:"Longest time the program may run; then it ends with all processes it started" schema:"format=runnel-duration,default=10m"`
}

func newRun(with map[string]any) (action.Action, error) {
	a := &runAction{}
	if err := decode(with, a, &a.Format); err != nil {
		return nil, err
	}
	if !proc.Valid(a.Encoding) {
		return nil, fmt.Errorf("invalid encoding %q (expected auto, utf-8, oem or windows-1252)", a.Encoding)
	}
	if !strings.Contains(a.MaxOutput, "${") {
		if _, err := outputLimit(a.MaxOutput); err != nil {
			return nil, err
		}
	}
	if _, err := a.timeout(); err != nil {
		return nil, err
	}
	for name := range a.Env {
		if err := checkEnvName(name); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *runAction) timeout() (time.Duration, error) { return parseTimeout(a.Timeout) }

func (a *runAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	base := paths.Base(ctx)
	program, err := proc.Resolve(a.Program, base)
	if err != nil {
		return action.Result{}, err
	}
	timeout, _ := a.timeout()
	limit, err := outputLimit(a.MaxOutput)
	if err != nil {
		return action.Result{}, err
	}
	cmd := proc.Command{Program: program, Args: a.Args, Dir: base, Encoding: a.Encoding, MaxOutput: limit, Timeout: timeout}
	if a.Workdir != "" {
		cmd.Dir = paths.Resolve(ctx, a.Workdir)
	}
	if a.Input != nil {
		cmd.Input, cmd.HasInput = *a.Input, true
	}
	ok := []int{0}
	if len(a.OKCodes) > 0 {
		ok = a.OKCodes
	}
	name := filepath.Base(program)
	ec.Log(slog.LevelDebug, fmt.Sprintf("starting %s with %d %s", name, len(a.Args), plural(len(a.Args), "argument")))
	return execute(ctx, ec, name, cmd, a.Env, ok, a.Format == "json")
}
