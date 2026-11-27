package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/scaffold"
)

func newInitCommand() *cobra.Command {
	var opts scaffold.Options
	cmd := &cobra.Command{
		Use:   "init [folder]",
		Short: "Create a new project",
		Long: "Creates a new project in folder (or the current directory; created if missing): " + model.ProjectFile + ",\n" +
			"the folders " + scaffold.WorkflowsDir + "/, " + scaffold.PipelinesDir + "/, " + model.TriggersDir + "/, " +
			model.EnvironmentsDir + "/ and " + model.ConnectionsDir + "/, the environment " + scaffold.Environment + ",\n" +
			"a small example workflow (" + scaffold.ExampleWorkflow + ") and .gitignore.\n\n" +
			"The project is named after the folder (in lower case, other characters than letters,\n" +
			"digits, -, _ and . replaced by -), or as given with --name.\n" +
			"A folder that is not empty (apart from .git) is refused; with --force the missing files\n" +
			"are added, existing files are never overwritten.\n\n" +
			"Exit codes: 0 project created, 1 a file could not be written,\n" +
			"2 nothing created (invalid name, folder not empty, invalid arguments).",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			return initProject(cmd, dir, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Name, "name", "", "name of the project (default: from the folder name)")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "add the files to a folder that is not empty; existing files are kept")
	return cmd
}

func initProject(cmd *cobra.Command, dir string, opts scaffold.Options) error {
	res, err := scaffold.Create(dir, opts)
	switch {
	case errors.Is(err, scaffold.ErrNotEmpty):
		return notChecked(fmt.Errorf("%s is not empty; use an empty folder, or --force to add the missing files (existing files are kept)", dir))
	case errors.Is(err, scaffold.ErrInvalidName), errors.Is(err, scaffold.ErrNotDir):
		return notChecked(err)
	case err != nil:
		return fmt.Errorf("create project in %s: %w", dir, err)
	}
	out := cmd.OutOrStdout()
	switch {
	case slices.Contains(res.Created, model.ProjectFile):
		_, _ = fmt.Fprintf(out, "Created project %q in %s\n", res.Name, dir)
	case len(res.Created) > 0:
		_, _ = fmt.Fprintf(out, "Added the missing project files to %s\n", dir)
	default:
		_, _ = fmt.Fprintf(out, "Nothing to add: the project files are already in %s\n", dir)
	}
	for _, p := range res.Created {
		_, _ = fmt.Fprintf(out, "  %s\n", filepath.FromSlash(p))
	}
	if len(res.Skipped) > 0 {
		_, _ = fmt.Fprintln(out, "Kept (already there):")
		for _, p := range res.Skipped {
			_, _ = fmt.Fprintf(out, "  %s\n", filepath.FromSlash(p))
		}
	}
	d := shellArg(dir)
	_, _ = fmt.Fprintf(out, "\nNext steps:\n  runnel validate %s\n  runnel run %s\n  runnel serve %s\n",
		d, shellArg(filepath.Join(dir, filepath.FromSlash(scaffold.ExampleWorkflow))), d)
	return nil
}

// shellArg quotes s for the command lines shown if it contains a space.
func shellArg(s string) string {
	if strings.ContainsAny(s, " \t") {
		return `"` + s + `"`
	}
	return s
}
