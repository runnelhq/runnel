package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
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
		if _, ok := errors.AsType[errExitCode](err); !ok {
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
	if _, err := newRunLogger(out, opts, "", nil); err != nil {
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
		if diags, ok := errors.AsType[model.Errors](err); ok {
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
	rl, _ := newRunLogger(out, opts, p.Pipeline.Name, p.Env)
	logger = &pipelineLogger{runLogger: *rl}
	return p.Runner, nil
}

// pipelineLogger prints the events of a pipeline run; every transform ends with its
// counts, so that the numbers are visible without -v.
type pipelineLogger struct {
	runLogger
}

func (l *pipelineLogger) event(e pipeline.Event) {
	if line, ok := runs.DescribePipeline(l.workflow, l.envName(), e); ok {
		l.show(e.Time, line)
	}
}
