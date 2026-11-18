// Package cli implements the runnel command line interface.
package cli

import (
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

	root.AddCommand(newVersionCommand())
	return root
}

// Execute runs the CLI with the given arguments and returns the process exit code.
func Execute(args []string, out, errOut io.Writer) int {
	root := NewRootCommand(out, errOut)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		root.PrintErrln("Error:", err)
		return 1
	}
	return 0
}
