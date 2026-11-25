package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/script"
)

// core.script runs JavaScript (ADR 0028) in a runtime without access to files, network
// or processes: inline or from a .js file, without variables in the code; values come
// only as params. The code is the body of a function: return gives result.output,
// console.log/warn/error write to the run log, an exception fails the action. The
// timeout of the action and cancelling the run interrupt the script.
var scriptDefinition = action.Definition{
	Type:        "core.script",
	Category:    action.CategoryExecution,
	Label:       "JavaScript",
	Description: "Runs JavaScript with params; output: the value of return",
	Example:     "params:\n  orders: \"{{ results.load.output.rows }}\"\n  rate: 0.19\nscript: |\n  const sum = params.orders.reduce((s, o) => s + Number(o.amount), 0);\n  console.log(`${params.orders.length} orders`);\n  return { net: sum, gross: sum * (1 + params.rate) };\n",
	Config:      scriptAction{},
	New:         newScript,
}

type scriptAction struct {
	Script string         `json:"script" doc:"JavaScript, the body of a function: params holds the values, return gives output; ${…} and {{ … }} in it are plain text" schema:"format=runnel-script"`
	File   string         `json:"file" doc:"A .js file instead of script, relative to the project folder, e.g. scripts/total.js" schema:"format=runnel-script"`
	Params map[string]any `json:"params" doc:"Values for the script as params.NAME; may use ${…} and {{ … }}, a value of its own may be ${secret:…}" schema:"format=runnel-params"`
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
	return a, nil
}

// maxScriptBytes limits the size of a script file.
const maxScriptBytes = 4 << 20

func (a *scriptAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	name, code := "script", a.Script
	if a.File != "" {
		name = filepath.ToSlash(a.File)
		file := paths.Resolve(ctx, a.File)
		info, err := os.Stat(file)
		switch {
		case err != nil || info.IsDir():
			return action.Result{}, fmt.Errorf("script file %s not found", a.File)
		case info.Size() > maxScriptBytes:
			return action.Result{}, fmt.Errorf("script file %s is larger than %d MiB", a.File, maxScriptBytes>>20)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return action.Result{}, fmt.Errorf("cannot read script file %s", a.File)
		}
		code = strings.TrimPrefix(string(data), "\uFEFF")
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
	out, err := rt.Call(ctx, fn, params)
	if err != nil {
		return action.Result{}, err
	}
	return action.Result{Output: out}, nil
}
