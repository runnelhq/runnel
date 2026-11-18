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

	"github.com/runnelhq/runnel/internal/model"
)

const workflowSuffix = ".workflow.yaml"

func newValidateCommand() *cobra.Command {
	var strict bool
	cmd := &cobra.Command{
		Use:   "validate [path...]",
		Short: "Check workflow files for errors",
		Long: "Checks workflow files (*" + workflowSuffix + ") for syntax errors, invalid fields,\n" +
			"links to unknown actions, cycles and unreachable actions.\n\n" +
			"Paths can be files or directories; directories are searched recursively.\n" +
			"Without a path, the current directory is checked.\n\n" +
			"Exit code 1 if an error was found (or a warning, with --strict).",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				args = []string{"."}
			}
			files, err := findWorkflowFiles(args)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				return fmt.Errorf("no workflow files (*%s) found in %s", workflowSuffix, strings.Join(args, ", "))
			}
			return validateFiles(cmd.OutOrStdout(), files, strict)
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "treat warnings as errors")
	return cmd
}

func validateFiles(out io.Writer, files []string, strict bool) error {
	var nErrors, nWarnings int
	for _, file := range files {
		diags, err := validateFile(file)
		if err != nil {
			return err
		}
		for _, d := range diags {
			_, _ = fmt.Fprintln(out, formatDiagnostic(d))
			if d.Severity == model.SeverityWarning {
				nWarnings++
			} else {
				nErrors++
			}
		}
	}
	_, _ = fmt.Fprintf(out, "%s checked: %s, %s\n",
		plural(len(files), "workflow file"), plural(nErrors, "error"), plural(nWarnings, "warning"))
	if nErrors > 0 || (strict && nWarnings > 0) {
		return errExitCode(1)
	}
	return nil
}

// validateFile loads and validates one file. The returned error is only set for
// problems unrelated to the content (e.g. the file cannot be read).
func validateFile(file string) (model.Errors, error) {
	w, err := model.LoadWorkflow(file)
	if err != nil {
		var diags model.Errors
		if errors.As(err, &diags) {
			return diags, nil
		}
		return nil, err
	}
	return w.Validate(), nil
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
