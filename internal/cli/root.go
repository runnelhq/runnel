// Package cli implements the runnel command line interface.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// NewRootCommand builds the root "runnel" command with all subcommands.
// Output is written to out, errors to errOut.
func NewRootCommand(out, errOut io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "runnel",
		Short: "Runnel – lightweight workflow and data integration",
		Long: "Runnel runs workflows and pipelines defined as YAML files.\n" +
			"See https://github.com/runnelhq/runnel for documentation.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(out)
	root.SetErr(errOut)

	root.AddCommand(newVersionCommand(), newValidateCommand(), newRunCommand(), newCatalogCommand(), newServeCommand(), newTriggersCommand(), newRunsCommand(), newServiceCommand(), newSchemaCommand())
	return root
}

// Execute runs the CLI with the given arguments and returns the process exit code.
func Execute(args []string, out, errOut io.Writer) int {
	return ExecuteContext(context.Background(), args, out, errOut)
}

// ExecuteContext is Execute with a context; cancelling it stops a running workflow.
func ExecuteContext(ctx context.Context, args []string, out, errOut io.Writer) int {
	root := NewRootCommand(out, errOut)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		if exit, ok := errors.AsType[errExitCode](err); ok {
			return int(exit)
		}
		root.PrintErrln("Error:", err)
		return 1
	}
	return 0
}

// errExitCode ends the command with the given exit code without printing an error;
// the command has already reported the details.
type errExitCode int

func (e errExitCode) Error() string { return fmt.Sprintf("exit code %d", int(e)) }
