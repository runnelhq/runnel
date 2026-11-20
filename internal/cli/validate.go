package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/runs"
	"github.com/runnelhq/runnel/internal/vars"
)

const workflowSuffix = ".workflow.yaml"

func newValidateCommand() *cobra.Command {
	var opts validateOptions
	cmd := &cobra.Command{
		Use:   "validate [path...]",
		Short: "Check workflow files for errors",
		Long: "Checks workflow files (*" + workflowSuffix + ") for syntax errors, invalid fields,\n" +
			"links to unknown actions, cycles, unreachable actions, undefined variables,\n" +
			"unknown action types and invalid action configurations (with:).\n\n" +
			"Paths can be files or directories; directories are searched recursively.\n" +
			"Without a path, the current directory is checked.\n\n" +
			"The project (project.yaml in the workflow's directory or above) and its environments\n" +
			"are checked as well. Variables are checked against every environment of the project,\n" +
			"or only against the one given with --env.\n\n" +
			"Exit code 1 if an error was found (or a warning, with --strict).",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				args = []string{"."}
			}
			params, err := parseParams(opts.params)
			if err != nil {
				return err
			}
			files, err := findWorkflowFiles(args)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				return fmt.Errorf("no workflow files (*%s) found in %s", workflowSuffix, strings.Join(args, ", "))
			}
			v := &validator{out: cmd.OutOrStdout(), opts: opts, params: params, projects: map[string]*model.Project{},
				registry: actions.Builtin()}
			return v.run(files)
		},
	}
	cmd.Flags().BoolVar(&opts.strict, "strict", false, "treat warnings as errors")
	cmd.Flags().StringVar(&opts.env, "env", "", "check variables only against this environment of the project")
	cmd.Flags().StringArrayVar(&opts.params, "param", nil, "parameter `KEY=VALUE` passed at start (repeatable)")
	return cmd
}

type validateOptions struct {
	strict bool
	env    string
	params []string
}

type validator struct {
	out    io.Writer
	opts   validateOptions
	params map[string]string
	// projects caches loaded projects by absolute directory; nil if loading failed.
	projects           map[string]*model.Project
	registry           *action.Registry
	nErrors, nWarnings int
}

func (v *validator) run(files []string) error {
	for _, file := range files {
		if err := v.file(file); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintf(v.out, "%s checked: %s, %s\n",
		plural(len(files), "workflow file"), plural(v.nErrors, "error"), plural(v.nWarnings, "warning"))
	if v.nErrors > 0 || (v.opts.strict && v.nWarnings > 0) {
		return errExitCode(1)
	}
	return nil
}

// file checks one workflow file. The returned error is only set for problems
// unrelated to the content (e.g. unreadable files or invalid command line flags).
func (v *validator) file(file string) error {
	w, err := model.LoadWorkflow(file)
	if err != nil {
		var diags model.Errors
		if !errors.As(err, &diags) {
			return err
		}
		v.report(diags)
		return nil
	}
	diags := w.Validate()

	p, found, err := v.project(filepath.Dir(file))
	if err != nil {
		return err
	}
	if !found && v.opts.env != "" {
		return fmt.Errorf("--env %s: no project (%s) found for %s", v.opts.env, model.ProjectFile, file)
	}
	if !found || p != nil {
		contexts, err := model.VarContexts(w, p, v.opts.env, v.params)
		if err != nil {
			return err
		}
		diags = append(diags, w.CheckVariables(contexts)...)
	}
	// Action types, configurations and calls of other workflows are only checked on an
	// otherwise valid workflow, like before a run.
	if !diags.HasErrors() {
		diags = append(diags, workflow.Check(w, workflow.Options{Registry: v.registry})...)
		projectDir := ""
		if root, found, _ := model.FindProjectDir(filepath.Dir(file)); found {
			projectDir = root
		}
		diags = append(diags, runs.CheckCalls(w, projectDir, file)...)
	}
	diags.Sort()
	v.report(diags)
	return nil
}

// project returns the project of a directory, loading and reporting it on first use.
// p is nil if no project was found or it could not be loaded.
func (v *validator) project(dir string) (p *model.Project, found bool, err error) {
	root, found, err := model.FindProjectDir(dir)
	if err != nil || !found {
		return nil, false, err
	}
	if p, seen := v.projects[root]; seen {
		return p, true, nil
	}
	p, err = model.LoadProject(displayPath(root))
	var diags model.Errors
	if errors.As(err, &diags) {
		v.report(diags)
		if diags.HasErrors() {
			p = nil
		}
	} else if err != nil {
		return nil, true, err
	}
	v.projects[root] = p
	return p, true, nil
}

func (v *validator) report(diags model.Errors) {
	for _, d := range diags {
		_, _ = fmt.Fprintln(v.out, formatDiagnostic(d))
		if d.Severity == model.SeverityWarning {
			v.nWarnings++
		} else {
			v.nErrors++
		}
	}
}

// parseParams parses "KEY=VALUE" arguments.
func parseParams(args []string) (map[string]string, error) {
	params := map[string]string{}
	for _, a := range args {
		k, val, ok := strings.Cut(a, "=")
		if !ok || !vars.ValidName(k) {
			return nil, fmt.Errorf("invalid --param %q (expected KEY=VALUE, KEY: letters, digits and _)", a)
		}
		params[k] = val
	}
	return params, nil
}

// displayPath shortens an absolute path to a path relative to the working directory, if it lies below it.
func displayPath(abs string) string {
	wd, err := os.Getwd()
	if err != nil {
		return abs
	}
	rel, err := filepath.Rel(wd, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return abs
	}
	return rel
}

func formatDiagnostic(d *model.Error) string {
	loc := d.File
	if d.Pos.Line != 0 {
		loc += ":" + d.Pos.String()
	}
	return fmt.Sprintf("%s: %s: %s", loc, d.Severity, d.Msg)
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// findWorkflowFiles expands the given paths: files are taken as they are,
// directories are searched recursively for workflow files (hidden directories are skipped).
func findWorkflowFiles(paths []string) ([]string, error) {
	var files []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			files = append(files, p)
			continue
		}
		err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && path != p && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(d.Name(), workflowSuffix) {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	slices.Sort(files)
	return slices.Compact(files), nil
}
