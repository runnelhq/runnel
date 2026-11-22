// Command runnel is the Runnel CLI: it validates and runs workflows and pipelines
// and serves the API for the editor.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/runnelhq/runnel/internal/cli"
)

func main() {
	os.Exit(run())
}

// run executes the command and returns the exit code; main exits after the deferred calls.
func run() int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// First Ctrl+C (or SIGTERM): cancel the run cleanly. Second: exit immediately.
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		fmt.Fprintln(os.Stderr, "Cancelling... (press Ctrl+C again to exit immediately)")
		cancel()
		<-signals
		os.Exit(cli.ExitCancelled)
	}()

	return cli.ExecuteContext(ctx, os.Args[1:], os.Stdout, os.Stderr)
}
