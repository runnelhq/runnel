package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/runs"
	"github.com/runnelhq/runnel/internal/transforms"
)

// onError runs the error workflow of a failed run of "runnel run" in this process and
// waits for it (ADR 0027): same environment and history, trigger error, the failed run as
// parent. Its result is printed; it does not change the exit code. Nothing happens if
// r is nil or the run has no failure with an error workflow.
func (r *cliRun) onError(ctx context.Context, errOut io.Writer) {
	if r == nil || r.failure == nil {
		return
	}
	f := *r.failure
	r.failure = nil
	file := filepath.Join(runs.BaseDir(r.projectDir, r.file), filepath.FromSlash(f.Target))
	data, err := os.ReadFile(file) //nolint:gosec // settings.onError of the project is checked to lie in the project (no .., not absolute)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "on error: %s could not be started: %v\n", f.Target, err)
		return
	}
	spec := runs.ErrorSpec(f, r.projectDir, file, data)
	spec.Name, spec.Registry, spec.Transforms = filepath.ToSlash(file), actions.Builtin(), transforms.Builtin()
	var logger *runLogger
	spec.Observer = func(e workflow.Event) { logger.event(e) }
	m := runs.NewManager()
	pend, p, err := m.PrepareRun(runPath(file, r.projectDir), spec)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "on error: %s could not be started: %v\n", f.Target, err)
		return
	}
	logger, _ = newRunLogger(r.out, r.opts, p.Workflow.Name, p.Env)
	_, _ = fmt.Fprintf(errOut, "on error: started %s (run %s)\n", f.Target, pend.ID())
	code := newCLIRun(m, pend, r.out, errOut, r.opts, r.projectDir, file, p.Project).execute(ctx, errOut)
	result := workflow.Succeeded
	if c, ok := errors.AsType[errExitCode](code); ok {
		result = workflow.Failed
		if c == ExitCancelled {
			result = workflow.Cancelled
		}
	}
	_, _ = fmt.Fprintf(errOut, "on error: %s %s (run %s)\n", f.Target, result, pend.ID())
}
