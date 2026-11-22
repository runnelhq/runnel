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

// InProject checks that a called file lies in the project folder (ADR 0008, Nachtrag
// S25b): calls cannot reach other files, also not with .. or an absolute path, so that
// a target built from values of a caller (e.g. parameters of an http trigger) stays in
// the project. Without a project folder every path is allowed. what is "workflow" or
// "pipeline", name the path for messages.
func InProject(projectDir, file, what, name string) error {
	if projectDir == "" {
		return nil
	}
	rel, err := filepath.Rel(absolute(projectDir), absolute(file))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("%s file %s lies outside the project folder; only files of the project can be called", what, name)
	}
	return nil
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
		if err := InProject(spec.ProjectDir, file, "workflow", name); err != nil {
			return nil, err
		}
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
// with the words of the log of a run (Describe).
func forward(log func(slog.Level, string)) func(workflow.Event) {
	if log == nil {
		return nil
	}
	name := ""
	return func(e workflow.Event) {
		kind := stepLine
		switch e.Kind {
		case workflow.RunStarted:
			name, kind = e.Message, startLine
		case workflow.RunFinished:
			kind = endLine
		case workflow.Log:
			kind = logLine
		}
		if l, ok := Describe(name, "", e); ok {
			nested(log, name, l, kind)
		}
	}
}

// lineKind tells nested what a line of a called run is about.
type lineKind int

const (
	stepLine  lineKind = iota // an action or transform started, waits, ended …
	logLine                   // a message logged by the run
	startLine                 // the run started
	endLine                   // the run ended
)

// nested writes a line of a called run to the log of the calling action. The calling
// action reports a failure itself, so problems of the called run are warnings and the
// end of the run is only told on success; the messages it logs keep their levels; all
// else is a detail (debug). Lines of steps are prefixed with "name/step: ".
func nested(log func(slog.Level, string), name string, l Line, kind lineKind) {
	level := l.Level
	switch {
	case kind == logLine:
	case kind == endLine && level > slog.LevelInfo:
		return
	case kind == endLine:
	case level >= slog.LevelWarn:
		level = slog.LevelWarn
	default:
		level = slog.LevelDebug
	}
	switch {
	case kind == startLine || kind == endLine:
		log(level, l.Msg)
	case l.Step == "":
		log(level, name+": "+l.Msg)
	default:
		log(level, name+"/"+l.Step+": "+l.Msg)
	}
}
