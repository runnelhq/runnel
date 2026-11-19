package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions/core"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/model"
)

// Exit codes of "runnel run".
const (
	exitRunFailed = 1 // the workflow ran and failed
	exitNotRun    = 2 // the workflow could not be started (invalid file, flags, …)
)

// newRegistry returns all action types built into this binary.
func newRegistry() *action.Registry {
	r := action.NewRegistry()
	core.Register(r)
	return r
}

type runOptions struct {
	env     string
	params  []string
	verbose bool
}

func newRunCommand() *cobra.Command {
	var opts runOptions
	cmd := &cobra.Command{
		Use:   "run <workflow-file>",
		Short: "Run a workflow",
		Long: "Runs a workflow file and prints its log.\n\n" +
			"Variables come from the workflow parameters, the project (project.yaml in the\n" +
			"workflow's directory or above), the environment (--env, default: defaultEnvironment\n" +
			"of the project) and --param, in this order of precedence (last wins).\n\n" +
			"Exit codes: 0 succeeded, 1 failed, 2 not started (invalid workflow or arguments).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflow(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.env, "env", "", "environment of the project to use")
	cmd.Flags().StringArrayVar(&opts.params, "param", nil, "parameter `KEY=VALUE` (repeatable)")
	cmd.Flags().BoolVarP(&opts.verbose, "verbose", "v", false, "also log debug messages and every action start and end")
	return cmd
}

func runWorkflow(ctx context.Context, out, errOut io.Writer, file string, opts runOptions) error {
	runner, err := prepareRun(out, errOut, file, opts)
	if err != nil {
		var exit errExitCode
		if !errors.As(err, &exit) {
			_, _ = fmt.Fprintln(errOut, "Error:", err)
		}
		return errExitCode(exitNotRun)
	}
	res := runner.Run(ctx)
	if res.Status != workflow.Succeeded {
		return errExitCode(exitRunFailed)
	}
	return nil
}

// prepareRun loads and checks everything needed for a run. Problems in files are
// printed to errOut and returned as errExitCode; other problems as plain errors.
// The run log is written to out.
func prepareRun(out, errOut io.Writer, file string, opts runOptions) (*workflow.Runner, error) {
	params, err := parseParams(opts.params)
	if err != nil {
		return nil, err
	}
	printDiags := func(diags model.Errors) {
		for _, d := range diags {
			_, _ = fmt.Fprintln(errOut, formatDiagnostic(d))
		}
	}

	w, err := model.LoadWorkflow(file)
	if err != nil {
		var diags model.Errors
		if errors.As(err, &diags) {
			printDiags(diags)
			return nil, errExitCode(exitNotRun)
		}
		return nil, err
	}
	diags := w.Validate()
	printDiags(diags)
	if diags.HasErrors() {
		return nil, errExitCode(exitNotRun)
	}

	var (
		p   *model.Project
		env *model.Environment
	)
	root, found, err := model.FindProjectDir(filepath.Dir(file))
	if err != nil {
		return nil, err
	}
	switch {
	case found:
		p, err = model.LoadProject(displayPath(root))
		if err != nil {
			var diags model.Errors
			if errors.As(err, &diags) {
				printDiags(diags)
				return nil, errExitCode(exitNotRun)
			}
			return nil, err
		}
		if env, err = p.SelectEnvironment(opts.env); err != nil {
			return nil, err
		}
	case opts.env != "":
		return nil, fmt.Errorf("--env %s: no project (%s) found for %s", opts.env, model.ProjectFile, file)
	}

	if err := checkParams(errOut, w, params); err != nil {
		return nil, err
	}

	logger := &runLogger{out: out, verbose: opts.verbose, workflow: w.Name, env: env}
	runner, err := workflow.Prepare(w, workflow.Options{
		Registry: newRegistry(),
		Scope:    model.Scope(w, p, env, params),
		Observer: logger.event,
	})
	if err != nil {
		var diags model.Errors
		if errors.As(err, &diags) {
			printDiags(diags)
			return nil, errExitCode(exitNotRun)
		}
		return nil, err
	}
	return runner, nil
}

// checkParams fails for missing required parameters and values that do not match
// the declared type or allowed values, and warns about undeclared parameters.
func checkParams(errOut io.Writer, w *model.Workflow, params map[string]string) error {
	declared := map[string]bool{}
	var problems, missing []string
	for _, p := range w.Parameters {
		declared[p.Name] = true
		value, given := params[p.Name]
		if !given {
			if p.Required && p.Default == "" {
				missing = append(missing, p.Name)
			}
			continue
		}
		if _, err := p.CheckValue(value); err != nil {
			problems = append(problems, fmt.Sprintf("parameter %s (%s): %v", p.Name, p.Type.OrString(), err))
		}
	}
	if len(missing) > 0 {
		problems = append(problems, fmt.Sprintf("missing required parameter(s) %v (set with --param NAME=VALUE)", missing))
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	var unknown []string
	for name := range params {
		if !declared[name] {
			unknown = append(unknown, name)
		}
	}
	slices.Sort(unknown)
	for _, name := range unknown {
		_, _ = fmt.Fprintf(errOut, "warning: parameter %q is not declared in workflow %q; it is available as variable ${%s}\n",
			name, w.Name, name)
	}
	return nil
}

// runLogger prints the events of a run as log lines.
type runLogger struct {
	out      io.Writer
	verbose  bool
	workflow string
	env      *model.Environment
}

func (l *runLogger) event(e workflow.Event) {
	switch e.Kind {
	case workflow.RunStarted:
		where := ""
		if l.env != nil {
			where = fmt.Sprintf(" (environment %s)", l.env.Name)
		}
		l.line(e.Time, slog.LevelInfo, "", fmt.Sprintf("workflow %q started%s, run %s", l.workflow, where, e.RunID))
	case workflow.ActionStarted:
		if l.verbose {
			l.line(e.Time, slog.LevelDebug, e.ActionID, "started ("+e.ActionType+")")
		}
	case workflow.Log:
		if e.Level >= slog.LevelInfo || l.verbose {
			l.line(e.Time, e.Level, e.ActionID, e.Message)
		}
	case workflow.ActionFinished:
		switch {
		case e.Err != nil:
			l.line(e.Time, slog.LevelError, e.ActionID, fmt.Sprintf("failed after %s: %v", round(e.Duration), e.Err))
		case l.verbose:
			l.line(e.Time, slog.LevelDebug, e.ActionID, fmt.Sprintf("%s in %s", e.Status, round(e.Duration)))
		}
	case workflow.RunFinished:
		for _, name := range sortedKeys(e.Outputs) {
			l.line(e.Time, slog.LevelInfo, "", fmt.Sprintf("output %s = %s", name, formatValue(e.Outputs[name])))
		}
		if e.Status == workflow.Succeeded {
			l.line(e.Time, slog.LevelInfo, "", fmt.Sprintf("workflow %q succeeded in %s", l.workflow, round(e.Duration)))
		} else {
			l.line(e.Time, slog.LevelError, "", fmt.Sprintf("workflow %q failed after %s: %v", l.workflow, round(e.Duration), e.Err))
		}
	}
}

func (l *runLogger) line(t time.Time, level slog.Level, actionID, msg string) {
	prefix := ""
	if actionID != "" {
		prefix = "[" + actionID + "] "
	}
	_, _ = fmt.Fprintf(l.out, "%s %-5s %s%s\n", t.Format("15:04:05.000"), level.String(), prefix, msg)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// formatValue shows output values as JSON, so that types are visible ("1" vs 1).
func formatValue(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}

func round(d time.Duration) time.Duration {
	if d < time.Second {
		return d.Round(time.Millisecond)
	}
	return d.Round(10 * time.Millisecond)
}
