package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/logfile"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/runs"
	"github.com/runnelhq/runnel/internal/transforms"
)

// Exit codes of "runnel run".
const (
	exitRunFailed = 1 // the workflow ran and failed
	exitNotRun    = 2 // the workflow could not be started (invalid file, flags, …)
	// ExitCancelled: the run was cancelled (Ctrl+C), as is customary for SIGINT.
	ExitCancelled = 130
)

type runOptions struct {
	env     string
	params  []string
	verbose bool
	log     logOptions
	// history (all, errors, none) overrides settings.history.cli; historyDir is the
	// folder of the run histories (ADR 0026, S26b).
	history, historyDir string
	// noOnError starts no error workflow when the run fails (ADR 0027).
	noOnError bool
}

func newRunCommand() *cobra.Command {
	var opts runOptions
	cmd := &cobra.Command{
		Use:   "run <workflow-or-pipeline-file>",
		Short: "Run a workflow or a pipeline",
		Long: "Runs a workflow (*" + workflowSuffix + ") or a pipeline (*" + pipelineSuffix + ") and prints its log;\n" +
			"for a pipeline, the number of records each transform read and wrote.\n\n" +
			"Variables come from the parameters, the project (project.yaml in the file's\n" +
			"directory or above), the environment (--env, default: defaultEnvironment\n" +
			"of the project) and --param, in this order of precedence (last wins).\n\n" +
			"Ctrl+C cancels the run cleanly (running actions and transforms are stopped); press it twice to exit at once.\n\n" +
			"The run is kept in the run history of the project (see \"runnel runs\"): all runs, only failed ones or none,\n" +
			"as set by --history or settings.history.cli in project.yaml (default all).\n\n" +
			"When the run fails, the error workflow of settings.onError (project.yaml, or the file itself) runs\n" +
			"afterwards in the same environment; the exit code stays that of the failed run. --no-on-error turns it off.\n\n" +
			"Exit codes: 0 succeeded, 1 failed, 2 not started (invalid file or arguments), 130 cancelled.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.HasSuffix(args[0], pipelineSuffix) {
				return runPipeline(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], opts)
			}
			return runWorkflow(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.env, "env", "", "environment of the project to use")
	cmd.Flags().StringArrayVar(&opts.params, "param", nil, "parameter `KEY=VALUE` (repeatable)")
	cmd.Flags().BoolVarP(&opts.verbose, "verbose", "v", false, "also log debug messages and every action or transform start and end (--log-level debug)")
	logFlags(cmd, &opts.log)
	cmd.Flags().StringVar(&opts.history, "history", "", "which ended runs are kept in the run history: all, errors or none; empty: settings.history.cli of the project, else all")
	historyDirFlag(cmd, &opts.historyDir, defaultHistoryHint)
	cmd.Flags().BoolVar(&opts.noOnError, "no-on-error", false, "start no error workflow (settings.onError) when the run fails")
	return cmd
}

func runWorkflow(ctx context.Context, out, errOut io.Writer, file string, opts runOptions) error {
	r, err := prepareRun(out, errOut, file, opts)
	if err != nil {
		if _, ok := errors.AsType[errExitCode](err); !ok {
			_, _ = fmt.Fprintln(errOut, "Error:", err)
		}
		r.onError(ctx, errOut) // r is set if the failure has an error workflow
		return errExitCode(exitNotRun)
	}
	return r.execute(ctx, errOut)
}

// prepareRun loads and checks everything needed for a run. Problems in files are
// printed to errOut and returned as errExitCode; other problems as plain errors.
// The run log is written to out.
func prepareRun(out, errOut io.Writer, file string, opts runOptions) (*cliRun, error) {
	params, err := checkRunOptions(out, opts)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(file); err == nil && info.IsDir() {
		return nil, fmt.Errorf("%s is a directory; pass a workflow file (*%s)", file, workflowSuffix)
	}
	data, projectDir, err := readRunFile(file, opts.env)
	if err != nil {
		return nil, err
	}

	var logger *runLogger
	m := runs.NewManager()
	path := runPath(file, projectDir)
	pend, p, err := m.PrepareRun(path, runs.Spec{
		Name:       filepath.ToSlash(file),
		Data:       data,
		ProjectDir: projectDir,
		Env:        opts.env,
		Params:     params,
		Registry:   actions.Builtin(),
		Transforms: transforms.Builtin(),
		Trigger:    runTriggerCLI,
		Observer:   func(e workflow.Event) { logger.event(e) },
		NoOnError:  opts.noOnError,
	})
	printDiags(errOut, p.Warnings)
	if err != nil {
		return notStarted(out, errOut, file, projectDir, path, data, opts, err)
	}
	for _, name := range p.Undeclared {
		_, _ = fmt.Fprintf(errOut, "warning: parameter %q is not declared in workflow %q; it is available as variable ${%s}\n",
			name, p.Workflow.Name, name)
	}
	logger, _ = newRunLogger(out, opts, p.Workflow.Name, p.Env)
	return newCLIRun(m, pend, out, errOut, opts, projectDir, file, p.Project), nil
}

// notStarted reports a run that could not be started: problems in files are printed and
// returned as errExitCode. The cliRun is set if the failure has an error workflow (ADR
// 0027), to run with its onError.
func notStarted(out, errOut io.Writer, file, projectDir, path string, data []byte, opts runOptions, err error) (*cliRun, error) {
	var r *cliRun
	if f, ok := runs.StartFailure(projectDir, path, data, runTriggerCLI, opts.env, opts.noOnError, err); ok {
		r = &cliRun{out: out, opts: opts, projectDir: projectDir, file: file, failure: &f}
	}
	if diags, ok := errors.AsType[model.Errors](err); ok {
		printDiags(errOut, diags)
		return r, errExitCode(exitNotRun)
	}
	if errors.Is(err, runs.ErrMissingParameters) {
		err = fmt.Errorf("%w (set with --param NAME=VALUE)", err)
	}
	return r, err
}

// readRunFile reads a workflow or pipeline file and finds its project ("" if none).
func readRunFile(file, env string) (data []byte, projectDir string, err error) {
	if data, err = os.ReadFile(file); err != nil {
		return nil, "", err
	}
	root, found, err := model.FindProjectDir(filepath.Dir(file))
	if err != nil {
		return nil, "", err
	}
	switch {
	case found:
		projectDir = displayPath(root)
	case env != "":
		return nil, "", fmt.Errorf("--env %s: no project (%s) found for %s", env, model.ProjectFile, file)
	}
	return data, projectDir, nil
}

func printDiags(errOut io.Writer, diags model.Errors) {
	for _, d := range diags {
		_, _ = fmt.Fprintln(errOut, formatDiagnostic(d))
	}
}

// runLogger prints the events of a run as log lines.
type runLogger struct {
	out      io.Writer
	level    slog.Level
	json     slog.Handler // --log-format json; nil: text
	workflow string
	env      *model.Environment
}

// newRunLogger returns the logger of the run log of "runnel run" (S71): --log-level
// (-v is debug) and --log-format.
func newRunLogger(out io.Writer, opts runOptions, name string, env *model.Environment) (*runLogger, error) {
	level, _, err := opts.log.check()
	if err != nil {
		return nil, err
	}
	if opts.verbose {
		level = min(level, slog.LevelDebug)
	}
	l := &runLogger{out: out, level: level, workflow: name, env: env}
	if opts.log.format == logfile.FormatJSON {
		l.json = slog.NewJSONHandler(out, &slog.HandlerOptions{Level: level})
	}
	return l, nil
}

func (l *runLogger) event(e workflow.Event) {
	if e.Kind == workflow.RunFinished {
		for _, name := range slices.Sorted(maps.Keys(e.Outputs)) {
			l.line(e.Time, slog.LevelInfo, "", fmt.Sprintf("output %s = %s", name, formatValue(e.Outputs[name])))
		}
	}
	if line, ok := runs.Describe(l.workflow, l.envName(), e); ok {
		l.show(e.Time, line)
	}
}

func (l *runLogger) envName() string {
	if l.env == nil {
		return ""
	}
	return l.env.Name
}

// show prints a line of the run; details (debug) only with -v or --log-level debug.
func (l *runLogger) show(t time.Time, line runs.Line) {
	if !line.Detail || l.level <= slog.LevelDebug {
		l.line(t, line.Level, line.Step, line.Msg)
	}
}

func (l *runLogger) line(t time.Time, level slog.Level, actionID, msg string) {
	if level < l.level {
		return
	}
	if l.json != nil {
		r := slog.NewRecord(t, level, msg, 0)
		if actionID != "" {
			r.AddAttrs(slog.String("action", actionID))
		}
		_ = l.json.Handle(context.Background(), r)
		return
	}
	prefix := ""
	if actionID != "" {
		prefix = "[" + actionID + "] "
	}
	_, _ = fmt.Fprintf(l.out, "%s %-5s %s%s\n", t.Format("15:04:05.000"), level.String(), prefix, msg)
}

// formatValue shows output values as JSON, so that types are visible ("1" vs 1).
func formatValue(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}
