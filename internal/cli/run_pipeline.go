package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/runnelhq/runnel/internal/engine/pipeline"
	"github.com/runnelhq/runnel/internal/runs"
	"github.com/runnelhq/runnel/internal/transforms"
)

// runPipeline runs a pipeline file (roadmap S13b) with the same flags and exit codes as a workflow.
func runPipeline(ctx context.Context, out, errOut io.Writer, file string, opts runOptions) error {
	r, err := preparePipelineRun(out, errOut, file, opts)
	if err != nil {
		if _, ok := errors.AsType[errExitCode](err); !ok {
			_, _ = fmt.Fprintln(errOut, "Error:", err)
		}
		r.onError(ctx, errOut) // r is set if the failure has an error workflow
		return errExitCode(exitNotRun)
	}
	return r.execute(ctx, errOut)
}

func preparePipelineRun(out, errOut io.Writer, file string, opts runOptions) (*cliRun, error) {
	params, err := checkRunOptions(out, opts)
	if err != nil {
		return nil, err
	}
	data, projectDir, err := readRunFile(file, opts.env)
	if err != nil {
		return nil, err
	}
	var logger *pipelineLogger
	m := runs.NewManager()
	path := runPath(file, projectDir)
	pend, p, err := m.PreparePipelineRun(path, runs.PipelineSpec{
		Name:       filepath.ToSlash(file),
		Data:       data,
		ProjectDir: projectDir,
		Env:        opts.env,
		Params:     params,
		Registry:   transforms.Builtin(),
		Trigger:    runTriggerCLI,
		Observer:   func(e pipeline.Event) { logger.event(e) },
		NoOnError:  opts.noOnError,
	})
	printDiags(errOut, p.Warnings)
	if err != nil {
		return notStarted(out, errOut, file, projectDir, path, data, opts, err)
	}
	for _, name := range p.Undeclared {
		_, _ = fmt.Fprintf(errOut, "warning: parameter %q is not declared in pipeline %q; it is available as variable ${%s}\n",
			name, p.Pipeline.Name, name)
	}
	rl, _ := newRunLogger(out, opts, p.Pipeline.Name, p.Env)
	logger = &pipelineLogger{runLogger: *rl}
	return newCLIRun(m, pend, out, errOut, opts, projectDir, file, p.Project), nil
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
