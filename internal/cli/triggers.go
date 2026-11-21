package cli

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/triggers"
)

func newTriggersCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "triggers [project-dir]",
		Short: "List the triggers of a project with their next time",
		Long: "Lists the triggers of the project in project-dir (or the current directory) – the files in\n" +
			model.TriggersDir + "/ – with their schedule, target and the next time they start a run.\n" +
			"Triggers start runs only while \"runnel serve\" runs. Problems of trigger files are\n" +
			"reported like by \"runnel validate\"; exit code 1 if there are any.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			root, found, err := model.FindProjectDir(dir)
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("no project (%s) found in %s or above", model.ProjectFile, dir)
			}
			list, problems := triggers.Load(displayPath(root))
			out := cmd.OutOrStdout()
			for _, p := range problems {
				_, _ = fmt.Fprintln(out, formatDiagnostic(p))
			}
			if len(list) == 0 {
				_, _ = fmt.Fprintf(out, "No triggers in %s\n", displayPath(root))
			} else {
				now := time.Now()
				tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
				_, _ = fmt.Fprintln(tw, "NAME\tTYPE\tSCHEDULE\tTARGET\tNEXT")
				for _, t := range list {
					info := triggers.InfoOf(t, time.Time{}, nil)
					next := "disabled"
					switch {
					case !t.IsEnabled():
					case t.Type == model.TriggerFile:
						next = "on new files"
					case t.Type == model.TriggerHTTP:
						next = "on request"
					default:
						next = t.Next(now).Format("2006-01-02 15:04 MST")
					}
					schedule := info.Schedule
					if info.Timezone != "" {
						schedule += " (" + info.Timezone + ")"
					}
					_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", t.Name, t.Type, schedule, t.Target, next)
				}
				_ = tw.Flush()
			}
			if problems.HasErrors() {
				return errExitCode(1)
			}
			return nil
		},
	}
}
