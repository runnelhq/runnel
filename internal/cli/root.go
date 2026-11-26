// Package cli implements the runnel command line interface.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// NewRootCommand builds the root "runnel" command with all subcommands.
// Output is written to out, errors to errOut.
func NewRootCommand(out, errOut io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "runnel",
		Short: "Runnel – lightweight workflow and data integration",
		Long: "Runnel runs workflows and pipelines defined as YAML files.\n" +
			"See https://github.com/runnelhq/runnel for documentation.\n\n" +
			"An invalid command line (unknown command or flag, missing or extra argument, flag without its value)\n" +
			"ends every command with exit code 2, \"runnel health\" with 3 (unknown); nothing is started.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err} })

	root.AddCommand(newVersionCommand(), newValidateCommand(), newRunCommand(), newCatalogCommand(), newServeCommand(), newTriggersCommand(), newRunsCommand(), newServiceCommand(), newSchemaCommand(), newPluginsCommand(), newHealthCommand())
	wrapArgs(root)
	return root
}

// exitUsage is the exit code of an invalid command line (R5a); "runnel health" uses
// its own (annotationUsageExit).
const exitUsage = 2

// annotationUsageExit in the annotations of a command sets its exit code for an invalid
// command line.
const annotationUsageExit = "runnel-usage-exit"

// usageError is an error in the command line: an unknown command or flag, a flag
// without its value, a missing or extra argument.
type usageError struct{ error }

func (e usageError) Unwrap() error { return e.error }

// wrapArgs makes the checks of the command line of cmd's subcommands (arguments,
// required and exclusive flags) return usageErrors; cobra runs the flag checks only
// after the hooks.
func wrapArgs(cmd *cobra.Command) {
	for _, sub := range cmd.Commands() {
		check := sub.Args
		sub.Args = func(c *cobra.Command, args []string) error {
			if check != nil {
				if err := check(c, args); err != nil {
					return usageError{err}
				}
			}
			if err := c.ValidateRequiredFlags(); err != nil {
				return usageError{err}
			}
			if err := c.ValidateFlagGroups(); err != nil {
				return usageError{err}
			}
			return nil
		}
		wrapArgs(sub)
	}
}

// usageExit returns the exit code of cmd for an invalid command line.
func usageExit(cmd *cobra.Command) int {
	for c := cmd; c != nil; c = c.Parent() {
		if v, ok := c.Annotations[annotationUsageExit]; ok {
			if n, err := strconv.Atoi(v); err == nil {
				return n
			}
		}
	}
	return exitUsage
}

// Execute runs the CLI with the given arguments and returns the process exit code.
func Execute(args []string, out, errOut io.Writer) int {
	return ExecuteContext(context.Background(), args, out, errOut)
}

// ExecuteContext is Execute with a context; cancelling it stops a running workflow.
func ExecuteContext(ctx context.Context, args []string, out, errOut io.Writer) int {
	root := NewRootCommand(out, errOut)
	root.SetArgs(args)
	cmd, err := root.ExecuteContextC(ctx)
	if err == nil {
		return 0
	}
	if exit, ok := errors.AsType[errExitCode](err); ok {
		return int(exit)
	}
	root.PrintErrln("Error:", err)
	if e, ok := errors.AsType[exitError](err); ok {
		return e.code
	}
	// cobra reports an unknown command before any check of ours runs.
	if _, ok := errors.AsType[usageError](err); ok || strings.HasPrefix(err.Error(), "unknown command ") {
		if cmd == nil {
			cmd = root
		}
		root.PrintErrf("Run '%s --help' for usage.\n", cmd.CommandPath())
		return usageExit(cmd)
	}
	return 1
}

// errExitCode ends the command with the given exit code without printing an error;
// the command has already reported the details.
type errExitCode int

func (e errExitCode) Error() string { return fmt.Sprintf("exit code %d", int(e)) }

// exitError is printed as "Error: …" and ends the command with its code.
type exitError struct {
	code int
	err  error
}

func (e exitError) Error() string { return e.err.Error() }
func (e exitError) Unwrap() error { return e.err }

// notChecked returns err (if set) with exit code 2: the command could not do its check
// (invalid arguments, missing path), as opposed to 1 for problems found (R5a).
func notChecked(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[errExitCode](err); ok {
		return err
	}
	return exitError{exitUsage, err}
}
