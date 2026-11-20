package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"

	"github.com/runnelhq/runnel/internal/engine/pipeline"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/runs"
	"github.com/runnelhq/runnel/internal/transforms"
)

// runPipeline runs a pipeline file (roadmap S13b) with the same flags and exit codes as a workflow.
func runPipeline(ctx context.Context, out, errOut io.Writer, file string, opts runOptions) error {
	runner, err := preparePipelineRun(out, errOut, file, opts)
	if err != nil {
		var exit errExitCode
		if !errors.As(err, &exit) {
			_, _ = fmt.Fprintln(errOut, "Error:", err)
		}
		return errExitCode(exitNotRun)
	}
	switch runner.Run(ctx).Status {
	case pipeline.Succeeded:
		return nil
	case pipeline.Cancelled:
		return errExitCode(ExitCancelled)
	}
	return errExitCode(exitRunFailed)
}

func preparePipelineRun(out, errOut io.Writer, file string, opts runOptions) (*pipeline.Runner, error) {
	params, err := parseParams(opts.params)
	if err != nil {
		return nil, err
	}
	data, projectDir, err := readRunFile(file, opts.env)
	if err != nil {
		return nil, err
	}
	var logger *pipelineLogger
	p, err := runs.PreparePipeline(runs.PipelineSpec{
		Name:       filepath.ToSlash(file),
		Data:       data,
		ProjectDir: projectDir,
		Env:        opts.env,
		Params:     params,
		Registry:   transforms.Builtin(),
		Observer:   func(e pipeline.Event) { logger.event(e) },
	})
	printDiags(errOut, p.Warnings)
	if err != nil {
		var diags model.Errors
		if errors.As(err, &diags) {
			printDiags(errOut, diags)
			return nil, errExitCode(exitNotRun)
		}
		if errors.Is(err, runs.ErrMissingParameters) {
			err = fmt.Errorf("%w (set with --param NAME=VALUE)", err)
		}
		return nil, err
	}
	for _, name := range p.Undeclared {
		_, _ = fmt.Fprintf(errOut, "warning: parameter %q is not declared in pipeline %q; it is available as variable ${%s}\n",
			name, p.Pipeline.Name, name)
	}
	logger = &pipelineLogger{runLogger: runLogger{out: out, verbose: opts.verbose, workflow: p.Pipeline.Name, env: p.Env}}
	return p.Runner, nil
}

// pipelineLogger prints the events of a pipeline run; every transform ends with its
// counts, so that the numbers are visible without -v.
type pipelineLogger struct {
	runLogger
}

func (l *pipelineLogger) event(e pipeline.Event) {
	name := l.workflow
	switch e.Kind {
	case pipeline.RunStarted:
		where := ""
		if l.env != nil {
			where = fmt.Sprintf(" (environment %s)", l.env.Name)
		}
		l.line(e.Time, slog.LevelInfo, "", fmt.Sprintf("pipeline %q started%s, run %s", name, where, e.RunID))
	case pipeline.TransformStarted:
		if l.verbose {
			l.line(e.Time, slog.LevelDebug, e.TransformID, "started ("+e.TransformType+")")
		}
	case pipeline.Log:
		if e.Level >= slog.LevelInfo || l.verbose {
			l.line(e.Time, e.Level, e.TransformID, e.Message)
		}
	case pipeline.TransformFinished:
		counts := fmt.Sprintf("read %d, written %d", e.Counts.Read, e.Counts.Written)
		if e.Counts.Errors > 0 {
			counts += fmt.Sprintf(", errors %d", e.Counts.Errors)
		}
		switch e.Status {
		case pipeline.Cancelled:
			l.line(e.Time, slog.LevelWarn, e.TransformID, fmt.Sprintf("cancelled after %s (%s)", round(e.Duration), counts))
		case pipeline.Failed:
			l.line(e.Time, slog.LevelError, e.TransformID, fmt.Sprintf("failed after %s (%s): %v", round(e.Duration), counts, e.Err))
		default:
			l.line(e.Time, slog.LevelInfo, e.TransformID, fmt.Sprintf("%s in %s", counts, round(e.Duration)))
		}
	case pipeline.RunFinished:
		switch e.Status {
		case pipeline.Succeeded:
			l.line(e.Time, slog.LevelInfo, "", fmt.Sprintf("pipeline %q succeeded in %s", name, round(e.Duration)))
		case pipeline.Cancelled:
			l.line(e.Time, slog.LevelWarn, "", fmt.Sprintf("pipeline %q cancelled after %s", name, round(e.Duration)))
		default:
			l.line(e.Time, slog.LevelError, "", fmt.Sprintf("pipeline %q failed after %s: %v", name, round(e.Duration), e.Err))
		}
	}
}
