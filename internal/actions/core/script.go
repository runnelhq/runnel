package core

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/script"
)

// core.script runs JavaScript (ADR 0028) in a runtime without access to files, network
// or processes: inline or from a .js file, without variables in the code; values come
// only as params. The code is the body of a function: return gives result.output,
// console.log/warn/error write to the run log, an exception fails the action. timeout
// (default 1m, R5b), the timeout of the action and cancelling the run interrupt the script.
var scriptDefinition = action.Definition{
	Type:        "core.script",
	Category:    action.CategoryExecution,
	Label:       "JavaScript",
	Description: "Runs JavaScript with params; output: the value of return",
	Example:     "params:\n  orders: \"{{ results.load.output.rows }}\"\n  rate: 0.19\nscript: |\n  const sum = params.orders.reduce((s, o) => s + Number(o.amount), 0);\n  console.log(`${params.orders.length} orders`);\n  return { net: sum, gross: sum * (1 + params.rate) };\n",
	Config:      scriptAction{},
	New:         newScript,
	Reference: action.Reference{
		Outputs: []action.OutputField{{Name: "output", Type: "any", Description: "The value of return, e.g. a number, text, list or object; null without return"}},
		Errors:  "Fails on a syntax error, an exception (with the line), a result that cannot be converted, a missing or too large script file, and when timeout, the timeout of the action or a cancelled run interrupts the script.",
		Limits: []string{
			"timeout limits the script (default 1m).",
			"The result may be at most 16 MiB as JSON and nested at most 100 levels; a script file or module may be at most 4 MiB.",
			"The console keeps at most 1,000 lines per call, each cut at 8 KiB.",
			"No access to files, network, processes or environment variables; require loads modules from scripts/ of the project only.",
		},
	},
}

type scriptAction struct {
	Script string         `json:"script" doc:"JavaScript, the body of a function: params holds the values, return gives output; ${…} and {{ … }} in it are plain text" schema:"format=runnel-script"`
	File   string         `json:"file" doc:"A .js file instead of script, relative to the project folder, e.g. scripts/total.js" schema:"format=runnel-script"`
	Params map[string]any `json:"params" doc:"Values for the script as params.NAME; may use ${…} and {{ … }}, a value of its own may be ${secret:…}" schema:"format=runnel-params"`
	// Timeout is a limit of its own, like that of shell.* (R5b): a script that never ends
	// does not hold the run.
	Timeout string `json:"timeout" doc:"Longest time the script may run, e.g. 5m" schema:"format=runnel-duration,default=1m"`
}

// defaultScriptTimeout is the default of timeout of core.script.
const defaultScriptTimeout = time.Minute

// timeout reads timeout of core.script.
func (a *scriptAction) timeout() (time.Duration, error) {
	if a.Timeout == "" {
		return defaultScriptTimeout, nil
	}
	d, err := time.ParseDuration(a.Timeout)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid timeout %q (examples: 30s, 5m)", a.Timeout)
	}
	return d, nil
}

func newScript(with map[string]any) (action.Action, error) {
	a := &scriptAction{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	switch {
	case a.Script == "" && a.File == "":
		return nil, errors.New("give the code in script or its file in file")
	case a.Script != "" && a.File != "":
		return nil, errors.New("give either script or file, not both")
	case a.File != "" && !strings.EqualFold(filepath.Ext(a.File), ".js"):
		return nil, fmt.Errorf("file %q: expected a .js file", a.File)
	}
	if _, err := a.timeout(); err != nil {
		return nil, err
	}
	// Syntax errors are found when checking, e.g. by runnel validate and in the editor (S73c).
	if a.Script != "" {
		if err := script.Check("script", a.Script, true, "params"); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *scriptAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	name, code := "script", a.Script
	if a.File != "" {
		var err error
		name = filepath.ToSlash(a.File)
		if code, err = script.ReadFile(paths.Resolve(ctx, a.File), a.File); err != nil {
			return action.Result{}, err
		}
	}
	rt := script.New(script.Options{Dir: paths.Base(ctx), Log: ec.Log})
	fn, err := rt.Compile(name, code, "params")
	if err != nil {
		return action.Result{}, err
	}
	params := a.Params
	if params == nil {
		params = map[string]any{}
	}
	timeout, _ := a.timeout()
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := rt.Call(callCtx, fn, params)
	if err != nil {
		if ctx.Err() == nil && callCtx.Err() != nil {
			return action.Result{}, fmt.Errorf("%s took longer than timeout (%s)", name, timeout)
		}
		return action.Result{}, err
	}
	return action.Result{Output: out}, nil
}
