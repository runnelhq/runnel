// Command runnel is the Runnel CLI: it validates and runs workflows and pipelines
// and (in later versions) serves the editor.
package main

import (
	"os"

	"github.com/runnelhq/runnel/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:], os.Stdout, os.Stderr))
}
