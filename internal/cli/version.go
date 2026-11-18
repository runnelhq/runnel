package cli

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/runnelhq/runnel/internal/version"
)

func newVersionCommand() *cobra.Command {
	var (
		short  bool
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := version.Get()
			switch {
			case asJSON:
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(info)
			case short:
				cmd.Println(info.Version)
			default:
				cmd.Println(info.String())
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&short, "short", false, "print only the version number")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print version information as JSON")
	cmd.MarkFlagsMutuallyExclusive("short", "json")
	return cmd
}
