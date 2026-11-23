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
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/runs"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms"
	"github.com/runnelhq/runnel/internal/triggers"
	"github.com/runnelhq/runnel/internal/vars"
)

const (
	workflowSuffix = ".workflow.yaml"
	pipelineSuffix = ".pipeline.yaml"
)

func newValidateCommand() *cobra.Command {
	var opts validateOptions
	cmd := &cobra.Command{
		Use:   "validate [path...]",
		Short: "Check workflow and pipeline files for errors",
		Long: "Checks workflow files (*" + workflowSuffix + ") for syntax errors, invalid fields,\n" +
			"links to unknown actions, cycles, unreachable actions, undefined variables,\n" +
			"unknown action types and invalid action configurations (with:).\n" +
			"Pipeline files (*" + pipelineSuffix + ") are checked for syntax errors, invalid fields,\n" +
			"links to unknown transforms, cycles, unknown transform types and invalid configurations.\n" +
			"Trigger files (" + model.TriggersDir + "/ of a project, also in subfolders) are checked for invalid fields and schedules,\n" +
			"missing targets and environments, and parameters the target does not declare.\n\n" +
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
			files, err := findFiles(args)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				return fmt.Errorf("no workflow, pipeline or trigger files (*%s, *%s, %s/**/*.yaml) found in %s", workflowSuffix, pipelineSuffix, model.TriggersDir, strings.Join(args, ", "))
			}
			v := &validator{out: cmd.OutOrStdout(), opts: opts, params: params, projects: map[string]*model.Project{},
				registry: actions.Builtin(), transforms: transforms.Builtin()}
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
	projects map[string]*model.Project
	// triggerNames are the names of the trigger files checked so far, by project directory.
	triggerNames       map[string]map[string]string
	registry           *action.Registry
	transforms         *transform.Registry
	nErrors, nWarnings int
}

func (v *validator) run(files []string) error {
	for _, file := range files {
		if err := v.file(file); err != nil {
			return err
		}
	}
	var workflows, pipelines, triggers int
	for _, f := range files {
		switch {
		case strings.HasSuffix(f, pipelineSuffix):
			pipelines++
		case isTriggerFile(f):
			triggers++
		default:
			workflows++
		}
	}
	var parts []string
	for _, c := range []struct {
		n    int
		noun string
	}{{workflows, "workflow file"}, {pipelines, "pipeline file"}, {triggers, "trigger file"}} {
		if c.n > 0 {
			parts = append(parts, plural(c.n, c.noun))
		}
	}
	checked := parts[len(parts)-1]
	if len(parts) > 1 {
		checked = strings.Join(parts[:len(parts)-1], ", ") + " and " + checked
	}
	_, _ = fmt.Fprintf(v.out, "%s checked: %s, %s\n", checked, plural(v.nErrors, "error"), plural(v.nWarnings, "warning"))
	if v.nErrors > 0 || (v.opts.strict && v.nWarnings > 0) {
		return errExitCode(1)
	}
	return nil
}

// file checks one workflow or pipeline file. The returned error is only set for problems
// unrelated to the content (e.g. unreadable files or invalid command line flags).
func (v *validator) file(file string) error {
	if strings.HasSuffix(file, pipelineSuffix) {
		return v.pipeline(file)
	}
	if isTriggerFile(file) {
		return v.trigger(file)
	}
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
	projectDir := ""
	if root, found, _ := model.FindProjectDir(filepath.Dir(file)); found {
		projectDir = root
	}
	diags, err = runs.CheckWorkflow(w, diags, runs.WorkflowCheck{Variables: !found || p != nil, Project: p,
		Env: v.opts.env, Params: v.params, Registry: v.registry, ProjectDir: projectDir, File: file})
	if err != nil {
		return err
	}
	v.report(diags)
	return nil
}

// pipeline checks a pipeline file: fields and graph (roadmap S12), then the types and
// configurations of its transforms (S13b), like before a run.
func (v *validator) pipeline(file string) error {
	p, err := model.LoadPipeline(file)
	if err != nil {
		var diags model.Errors
		if !errors.As(err, &diags) {
			return err
		}
		v.report(diags)
		return nil
	}
	project, _, err := v.project(filepath.Dir(file))
	if err != nil {
		return err
	}
	v.report(runs.CheckPipeline(p, p.Validate(), v.transforms, project))
	return nil
}

// trigger checks a trigger file: its fields and schedule, and against the project its
// target, environment and parameters (roadmap S37).
func (v *validator) trigger(file string) error {
	t, err := model.LoadTrigger(file)
	if err != nil {
		var diags model.Errors
		if !errors.As(err, &diags) {
			return err
		}
		v.report(diags)
		return nil
	}
	root, _ := model.TriggerProjectDir(file)
	p, _, err := v.project(root)
	if err != nil {
		return err
	}
	diags := triggers.CheckTrigger(t, p, root)
	if v.triggerNames == nil {
		v.triggerNames = map[string]map[string]string{}
	}
	names := v.triggerNames[root]
	if names == nil {
		names = map[string]string{}
		v.triggerNames[root] = names
	}
	if first, dup := names[t.Name]; dup {
		diags = append(diags, t.ErrorAt("$.name", "trigger name %q is already used by %s", t.Name, model.TriggerFileName(root, first)))
	} else {
		names[t.Name] = file
	}
	diags.Sort()
	v.report(diags)
	return nil
}

// isTriggerFile reports whether a file is in the triggers folder of a project (also in
// a subfolder of it).
func isTriggerFile(file string) bool {
	_, ok := model.TriggerProjectDir(file)
	return ok
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
	if diags, ok := errors.AsType[model.Errors](err); ok {
		v.report(diags)
		if diags.HasErrors() {
			p = nil
		}
	} else if err != nil {
		return nil, true, err
	}
	if p != nil {
		v.report(p.InlineSecretWarnings())
		v.report(runs.CheckConnections(p, v.opts.env))
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

// findFiles expands the given paths: files are taken as they are,
// directories are searched recursively for workflow and pipeline files (hidden directories are skipped).
func findFiles(paths []string) ([]string, error) {
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
			if !d.IsDir() && (strings.HasSuffix(d.Name(), workflowSuffix) || strings.HasSuffix(d.Name(), pipelineSuffix) || isTriggerFile(path)) {
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
