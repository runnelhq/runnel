package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/history"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/runs"
)

// runTriggerCLI is the trigger of runs of "runnel run" in the run history (ADR 0026).
const runTriggerCLI = "cli"

// checkRunOptions checks the options of "runnel run" before anything is loaded.
func checkRunOptions(out io.Writer, opts runOptions) (map[string]string, error) {
	params, err := parseParams(opts.params)
	if err != nil {
		return nil, err
	}
	if _, err := newRunLogger(out, opts, "", nil); err != nil {
		return nil, err
	}
	if err := checkHistoryMode("--history", opts.history); err != nil {
		return nil, err
	}
	return params, nil
}

func checkHistoryMode(name, mode string) error {
	switch mode {
	case "", model.HistoryAll, model.HistoryErrors, model.HistoryNone:
		return nil
	}
	return fmt.Errorf("invalid %s %q (expected all, errors or none)", name, mode)
}

// runPath is the path of the run file as the run history and the server show it:
// relative to the project with "/", or as given without a project.
func runPath(file, projectDir string) string {
	if projectDir != "" {
		if root, err := filepath.Abs(projectDir); err == nil {
			if abs, err := filepath.Abs(file); err == nil {
				if rel, err := filepath.Rel(root, abs); err == nil {
					return filepath.ToSlash(rel)
				}
			}
		}
	}
	return filepath.ToSlash(file)
}

// cliRun is a prepared run of "runnel run": its manager keeps it in the project's run
// history (S26b).
type cliRun struct {
	m        *runs.Manager
	pend     *runs.Pending
	store    *history.Store
	settings model.HistorySettings
	// out, opts, projectDir and file are for the error workflow (ADR 0027); failure is
	// set when the run failed and has one.
	out        io.Writer
	opts       runOptions
	projectDir string
	file       string
	failure    *runs.Failure
}

// newCLIRun chooses which runs are kept (--history, else settings.history.cli, else
// all) and opens the project's run history. A run without project, or with history
// none, opens nothing; a history that cannot be opened is a warning, the run goes on.
func newCLIRun(m *runs.Manager, pend *runs.Pending, out, errOut io.Writer, opts runOptions, projectDir, file string, project *model.Project) *cliRun {
	r := &cliRun{m: m, pend: pend, out: out, opts: opts, projectDir: projectDir, file: file}
	if project != nil {
		r.settings = project.Settings.History
	}
	mode := opts.history
	if mode == "" && project != nil {
		mode = project.Settings.History.CLI
	}
	if mode == "" {
		mode = model.HistoryAll
	}
	pend.History = mode
	if projectDir == "" || mode == model.HistoryNone {
		return r
	}
	warn := func(msg string) { _, _ = fmt.Fprintln(errOut, "warning: "+msg) }
	base := opts.historyDir
	var err error
	if base == "" {
		base, err = history.DefaultBase()
	}
	var dir string
	if err == nil {
		dir, err = history.ProjectDir(base, projectDir)
	}
	if err == nil {
		r.store, err = history.Open(context.Background(), dir)
	}
	if err != nil {
		warn(fmt.Sprintf("the run is not kept in the run history: %v", err))
		return r
	}
	m.SetHistory(r.store, warn)
	return r
}

// execute runs it until it ends, and then its error workflow if it failed (ADR 0027);
// Ctrl+C (ctx) cancels it. The error is the exit code, that of the run.
func (r *cliRun) execute(ctx context.Context, errOut io.Writer) error {
	code := r.launch(ctx, errOut)
	r.onError(ctx, errOut)
	return code
}

// launch runs it until it ends and writes it to the history.
func (r *cliRun) launch(ctx context.Context, errOut io.Writer) error {
	defer r.close()
	run, err := r.m.Launch(r.pend)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "Error:", err)
		return errExitCode(exitNotRun)
	}
	stop := context.AfterFunc(ctx, func() { r.m.Cancel(run.ID) })
	defer stop()
	d, _ := r.m.Wait(context.Background(), run.ID)
	switch d.Status {
	case workflow.Succeeded:
		return nil
	case workflow.Cancelled:
		return errExitCode(ExitCancelled)
	}
	if target := r.pend.OnError(); target != "" {
		f := runs.FailureOf(d, target)
		r.failure = &f
	}
	return errExitCode(exitRunFailed)
}

// close writes the run to the history, removes old runs (S26c) and closes it.
func (r *cliRun) close() {
	_ = r.m.Shutdown(context.Background())
	r.m.CloseHistory()
	if r.store != nil {
		days, maxRuns := r.settings.Retention()
		_, _ = r.store.Prune(context.Background(), days, maxRuns, time.Now()) // best effort: the next run tries again
		_ = r.store.Close()
	}
}
