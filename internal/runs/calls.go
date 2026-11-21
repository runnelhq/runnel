package runs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/model"
)

// Calls of other workflows (core.run-workflow, roadmap S11): the called workflow runs
// in the same project and environment, gets only the parameters given by the action
// (undeclared ones are an error) and returns its outputs. Its log goes to the log of
// the calling action, prefixed with "<workflow>/<action>: ".

// caller is a workflow in the chain of calls, to detect workflows calling themselves.
type caller struct {
	file string // absolute path
	name string // for messages
}

// Resolve returns the file a path in core.run-workflow refers to, and its name for
// messages: relative to the project folder, or without a project to the folder of
// the calling workflow file.
func Resolve(projectDir, callerFile, path string) (file, name string) {
	if filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return path, filepath.ToSlash(path)
	}
	if projectDir != "" {
		return filepath.Join(projectDir, path), filepath.ToSlash(filepath.Clean(path))
	}
	file = filepath.Join(filepath.Dir(callerFile), path)
	return file, filepath.ToSlash(file)
}

// absolute returns an absolute, cleaned path for comparisons (case-insensitive on Windows).
func absolute(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if runtime.GOOS == "windows" {
		return strings.ToLower(filepath.Clean(path))
	}
	return filepath.Clean(path)
}

// callChain describes the calls leading to a workflow, e.g. "a → b → a".
func callChain(chain []caller, last string) string {
	names := make([]string, 0, len(chain)+1)
	for _, c := range chain {
		names = append(names, c.name)
	}
	return strings.Join(append(names, last), " → ")
}

// workflows returns the runner for workflows called by the run of spec.
func (spec Spec) workflows(envName string) action.WorkflowRunner {
	chain := append(append([]caller(nil), spec.chain...), caller{file: absolute(spec.file()), name: spec.Name})
	return func(ctx context.Context, call action.WorkflowCall) (map[string]any, error) {
		file, name := Resolve(spec.ProjectDir, spec.file(), call.Path)
		abs := absolute(file)
		for _, c := range chain {
			if c.file == abs {
				return nil, fmt.Errorf("workflow %s calls itself (%s)", name, callChain(chain, name))
			}
		}
		data, err := os.ReadFile(file)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("workflow file %s not found", name)
		} else if err != nil {
			return nil, fmt.Errorf("cannot read workflow file %s: %w", name, err)
		}
		child := Spec{
			Name: name, Data: data, ProjectDir: spec.ProjectDir, Env: envName, Params: call.Params,
			Registry: spec.Registry, Transforms: spec.Transforms, Observer: forward(call.Log), File: file, chain: chain, strict: true,
			Request: spec.Request, Respond: spec.Respond,
		}
		p, err := Prepare(child)
		if err != nil {
			var diags model.Errors
			if errors.As(err, &diags) {
				return nil, fmt.Errorf("workflow %s cannot be started: %s", name, onlyErrors(diags))
			}
			return nil, fmt.Errorf("workflow %s cannot be started: %w", name, err)
		}
		res := p.Runner.Run(ctx)
		switch res.Status {
		case workflow.Succeeded:
			return res.Outputs, nil
		case workflow.Cancelled:
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		return nil, fmt.Errorf("workflow %s failed: %w", name, res.Err)
	}
}

func onlyErrors(diags model.Errors) string {
	var msgs []string
	for _, d := range diags {
		if d.Severity != model.SeverityWarning {
			msgs = append(msgs, d.Error())
		}
	}
	return strings.Join(msgs, "; ")
}

// forward writes the events of a called workflow to the log of the calling action,
// with the same words and levels as the log of a run.
func forward(log func(slog.Level, string)) func(workflow.Event) {
	if log == nil {
		return nil
	}
	name := ""
	return func(e workflow.Event) {
		at := name + "/" + e.ActionID + ": "
		if e.ActionID == "" {
			at = name + ": " // a message of the run itself
		}
		switch e.Kind {
		case workflow.RunStarted:
			name = e.Message
			log(slog.LevelDebug, fmt.Sprintf("workflow %q started, run %s", name, e.RunID))
		case workflow.ActionStarted:
			log(slog.LevelDebug, at+"started ("+e.ActionType+")")
		case workflow.Log:
			log(e.Level, at+e.Message)
		case workflow.ActionWaiting:
			log(slog.LevelDebug, at+e.Message)
		case workflow.ActionSkipped:
			log(slog.LevelDebug, at+"skipped (no incoming link applies)")
		case workflow.ActionFinished:
			switch {
			case e.Status == workflow.Cancelled:
				log(slog.LevelWarn, at+"cancelled after "+round(e.Duration).String())
			case e.Err != nil:
				log(slog.LevelWarn, fmt.Sprintf("%sfailed after %s: %v", at, round(e.Duration), e.Err))
			default:
				log(slog.LevelDebug, fmt.Sprintf("%s%s in %s", at, e.Status, round(e.Duration)))
			}
		case workflow.RunFinished:
			if e.Status == workflow.Succeeded {
				log(slog.LevelInfo, fmt.Sprintf("workflow %q succeeded in %s", name, round(e.Duration)))
			}
		}
	}
}

func round(d time.Duration) time.Duration {
	if d < time.Second {
		return d.Round(time.Millisecond)
	}
	return d.Round(10 * time.Millisecond)
}
