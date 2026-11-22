package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/schema"
)

func newSchemaCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "schema <kind>",
		Short: "Print the JSON Schema of a file kind (project, environment, trigger)",
		Long: "Prints the JSON Schema of a file kind: its fields with type, description, allowed\n" +
			"values and defaults, as the editor and the server (/api/v1/schemas/<kind>) use it.\n" +
			"Editors with JSON Schema support for YAML can use it, e.g. VS Code with a line\n" +
			"\"# yaml-language-server: $schema=<file>\" at the top of project.yaml.",
		Example: "  runnel schema project\n" +
			"  runnel schema trigger > trigger.schema.json",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind := strings.ToLower(args[0])
			format, ok := model.FileFormat(kind)
			if !ok {
				return fmt.Errorf("unknown file kind %q (expected %s)", args[0], strings.Join(model.FileFormats(), ", "))
			}
			s, err := schema.ForFile(format)
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), s)
		},
	}
}
