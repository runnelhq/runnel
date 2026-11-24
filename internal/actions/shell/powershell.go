package shell

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/proc"
	"github.com/runnelhq/runnel/internal/record"
)

// shell.powershell runs a PowerShell script (ADR 0023): inline or from a file, without
// variables in the script; values come only as params, passed as JSON to a start script
// that splats them as named parameters, so that their types stay. Output like shell.run;
// with json, the last JSON value of stdout (e.g. of ConvertTo-Json).
var powershellDefinition = action.Definition{
	Type:        "shell.powershell",
	Category:    action.CategoryExecution,
	Label:       "PowerShell",
	Description: "Runs a PowerShell script with named parameters; output: exitCode, stdout, stderr and optionally JSON",
	Example:     "script: |\n  param([string]$Folder)\n  Get-ChildItem $Folder | Select-Object Name, Length | ConvertTo-Json\nparams:\n  Folder: data\njson: true\n",
	Config:      powershellAction{},
	New:         newPowershell,
}

//go:embed runner.ps1
var runnerScript string

// Editions of PowerShell.
const (
	editionAuto    = "auto"
	editionPwsh    = "pwsh"
	editionWindows = "windows"
)

type powershellAction struct {
	Script    string            `json:"script" doc:"The script; no ${…} or {{ … }} – values come as params" schema:"format=runnel-script"`
	File      string            `json:"file" doc:"A script file instead of script, relative to the project folder, e.g. scripts/cleanup.ps1" schema:"format=runnel-script"`
	Params    map[string]any    `json:"params" doc:"Named parameters of the script (param(...)); numbers, true/false, lists and objects keep their type; may use ${…} and {{ … }}"`
	JSON      bool              `json:"json" doc:"Read the last JSON value of stdout (e.g. of ConvertTo-Json) into output.json" schema:"default=false"`
	Edition   string            `json:"edition" doc:"auto: PowerShell 7 (pwsh) if installed, else Windows PowerShell 5.1; pwsh or windows to choose one" schema:"enum=auto|pwsh|windows,default=auto"`
	Workdir   string            `json:"workdir" doc:"Working folder, relative to the project folder; empty: the project folder"`
	Env       map[string]string `json:"env" doc:"Further environment variables, e.g. secrets ($env:NAME in the script)" schema:"format=runnel-secret"`
	MaxOutput string            `json:"maxOutput" doc:"Largest size of stdout and of stderr kept, e.g. 500KB or 10MB; more is dropped (truncated)" schema:"default=1MB"`
	Timeout   string            `json:"timeout" doc:"Longest time the script may run; then it ends with all processes it started" schema:"format=runnel-duration,default=10m"`
}

func newPowershell(with map[string]any) (action.Action, error) {
	a := &powershellAction{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	switch {
	case a.Script == "" && a.File == "":
		return nil, errors.New("give the script in script or its file in file")
	case a.Script != "" && a.File != "":
		return nil, errors.New("give either script or file, not both")
	}
	switch a.Edition {
	case "", editionAuto, editionPwsh, editionWindows:
	default:
		return nil, fmt.Errorf("invalid edition %q (expected auto, pwsh or windows)", a.Edition)
	}
	if !strings.Contains(a.MaxOutput, "${") {
		if _, err := outputLimit(a.MaxOutput); err != nil {
			return nil, err
		}
	}
	if _, err := parseTimeout(a.Timeout); err != nil {
		return nil, err
	}
	for name := range a.Env {
		if err := checkEnvName(name); err != nil {
			return nil, err
		}
	}
	if _, err := json.Marshal(a.Params); err != nil {
		return nil, fmt.Errorf("params: %w", err)
	}
	return a, nil
}

// findPowerShell returns the program of an edition.
func findPowerShell(edition string) (string, error) {
	if edition != editionWindows {
		if p, err := exec.LookPath("pwsh"); err == nil {
			return p, nil
		}
		if edition == editionPwsh {
			return "", errors.New("PowerShell 7 (pwsh) is not installed")
		}
	}
	if runtime.GOOS != "windows" {
		return "", errors.New("the edition windows (powershell.exe) exists only on Windows; install PowerShell 7 (pwsh)")
	}
	p, err := exec.LookPath("powershell.exe")
	if err != nil {
		return "", errors.New("edition windows: powershell.exe not found")
	}
	return p, nil
}

func (a *powershellAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	program, err := findPowerShell(a.Edition)
	if err != nil {
		return action.Result{}, err
	}
	dir, err := os.MkdirTemp("", "runnel-ps-")
	if err != nil {
		return action.Result{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	// UTF-8 with BOM: Windows PowerShell 5.1 reads files without it in the ANSI code page.
	write := func(name, text string) (string, error) {
		p := filepath.Join(dir, name)
		return p, os.WriteFile(p, append([]byte{0xEF, 0xBB, 0xBF}, text...), 0o600)
	}
	runner, err := write("runner.ps1", runnerScript)
	if err != nil {
		return action.Result{}, err
	}
	script := paths.Resolve(ctx, a.File)
	if a.File == "" {
		if script, err = write("script.ps1", a.Script); err != nil {
			return action.Result{}, err
		}
	} else if _, err := os.Stat(script); err != nil {
		return action.Result{}, fmt.Errorf("script file %s not found", a.File)
	}
	params := a.Params
	if params == nil {
		params = map[string]any{}
	}
	input, err := json.Marshal(record.JSONValue(params))
	if err != nil {
		return action.Result{}, fmt.Errorf("params: %w", err)
	}
	timeout, _ := parseTimeout(a.Timeout)
	limit, err := outputLimit(a.MaxOutput)
	if err != nil {
		return action.Result{}, err
	}
	cmd := proc.Command{
		Program:   program,
		Args:      []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", runner, script},
		Dir:       paths.Base(ctx),
		Input:     string(input),
		HasInput:  true,
		Encoding:  proc.EncodingUTF8,
		MaxOutput: limit,
		Timeout:   timeout,
	}
	if a.Workdir != "" {
		cmd.Dir = paths.Resolve(ctx, a.Workdir)
	}
	name := "the script"
	if a.File != "" {
		name = filepath.Base(a.File)
	}
	ec.Log(slog.LevelDebug, fmt.Sprintf("starting %s with %s and %d %s", name, filepath.Base(program), len(params), plural(len(params), "parameter")))
	return execute(ctx, ec, name, cmd, a.Env, []int{0}, a.JSON)
}

// parseTimeout reads a timeout; empty: 10 minutes (ADR 0023).
func parseTimeout(s string) (time.Duration, error) {
	if s == "" {
		return 10 * time.Minute, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid timeout %q (expected e.g. 30s or 10m)", s)
	}
	return d, nil
}
