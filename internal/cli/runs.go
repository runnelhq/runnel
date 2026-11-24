package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/runnelhq/runnel/internal/history"
	"github.com/runnelhq/runnel/internal/runs"
)

// runID matches the id of a run (a ULID), to tell it from a project folder.
var runID = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

type runsOptions struct {
	filter     history.Filter
	json       bool
	historyDir string
}

func newRunsCommand() *cobra.Command {
	var o runsOptions
	cmd := &cobra.Command{
		Use:   "runs [project-dir] [run-id]",
		Short: "List the runs of a project from its run history",
		Long: "Lists the runs of the project in project-dir (or the current directory) from its run history,\n" +
			"newest first: those of \"runnel serve\", the service and \"runnel run\" (ADR 0026). With a run id,\n" +
			"shows that run with the state of its actions or transforms and its log.\n\n" +
			"The service keeps its history in %ProgramData%\\runnel\\history; to read it, pass that folder\n" +
			"with --history-dir (as administrator).",
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, id := ".", ""
			switch {
			case len(args) == 2:
				dir, id = args[0], args[1]
			case len(args) == 1 && runID.MatchString(args[0]) && !isDir(args[0]):
				id = args[0]
			case len(args) == 1:
				dir = args[0]
			}
			if o.filter.Limit < 1 || o.filter.Limit > runs.MaxList {
				return fmt.Errorf("invalid --limit %d (expected 1–%d)", o.filter.Limit, runs.MaxList)
			}
			if err := checkHistoryStatus(o.filter.Status); err != nil {
				return err
			}
			return listRuns(cmd.Context(), cmd.OutOrStdout(), dir, id, o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.filter.Status, "status", "", "only runs with this status: running, succeeded, failed or cancelled")
	f.StringVar(&o.filter.Path, "path", "", "only runs of this file, relative to the project (e.g. workflows/import.workflow.yaml)")
	f.StringVar(&o.filter.Trigger, "trigger", "", "only runs started by this trigger, e.g. cron:nightly, http:orders or cli")
	f.StringVar(&o.filter.Before, "before", "", "only runs started before the run with this id (the next page)")
	f.IntVar(&o.filter.Limit, "limit", 20, "most runs listed")
	f.BoolVar(&o.json, "json", false, "print the runs as JSON, as the API (GET /api/v1/runs) returns them")
	historyDirFlag(cmd, &o.historyDir, defaultHistoryHint)
	return cmd
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func checkHistoryStatus(s string) error {
	switch s {
	case "", "running", "succeeded", "failed", "cancelled":
		return nil
	}
	return fmt.Errorf("invalid --status %q (expected running, succeeded, failed or cancelled)", s)
}

// listRuns prints the runs of the project in dir, or the run id.
func listRuns(ctx context.Context, out io.Writer, dir, id string, o runsOptions) error {
	root, err := findProject(dir)
	if err != nil {
		return err
	}
	base := o.historyDir
	if base == "" {
		if base, err = history.DefaultBase(); err != nil {
			return err
		}
	}
	hdir, err := history.ProjectDir(base, root)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(hdir, history.FileName)); errors.Is(err, os.ErrNotExist) {
		// Reading creates nothing: a project without runs has no history yet.
		if id != "" {
			return fmt.Errorf("run %s not found: the project has no run history in %s", id, hdir)
		}
		if o.json {
			return writeJSON(out, map[string][]runs.Run{"runs": {}})
		}
		_, _ = fmt.Fprintf(out, "No runs in the history of %s (%s)\n", displayPath(root), hdir)
		return nil
	}
	store, err := history.Open(ctx, hdir)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	m := runs.NewManager()
	m.SetHistory(store, nil)
	defer m.CloseHistory()

	if id != "" {
		d, ok, err := m.Get(ctx, id)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("run %s not found in the history of %s", id, displayPath(root))
		}
		if o.json {
			return writeJSON(out, d)
		}
		printRun(out, d)
		return nil
	}
	list, err := m.Find(ctx, o.filter)
	if err != nil {
		return err
	}
	if o.json {
		return writeJSON(out, map[string][]runs.Run{"runs": list})
	}
	if len(list) == 0 {
		_, _ = fmt.Fprintf(out, "No runs in the history of %s match\n", displayPath(root))
		return nil
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "RUN\tSTARTED\tSTATUS\tDURATION\tTRIGGER\tPATH")
	for _, r := range list {
		trigger := r.Trigger
		if trigger == "" {
			trigger = "editor"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, r.Started.Local().Format(time.DateTime), r.Status,
			duration(r), trigger, r.Path)
	}
	_ = w.Flush()
	if len(list) == o.filter.Limit {
		_, _ = fmt.Fprintf(out, "More with --before %s\n", list[len(list)-1].ID)
	}
	return nil
}

func duration(r runs.Run) string {
	if r.Finished == nil {
		return "-"
	}
	return runs.Round(time.Duration(r.DurationMs) * time.Millisecond).String()
}

// printRun prints a run with its actions and log.
func printRun(out io.Writer, d runs.Detail) {
	_, _ = fmt.Fprintf(out, "Run %s of %s (%s %q)\n", d.ID, d.Path, d.Kind, d.Workflow)
	_, _ = fmt.Fprintf(out, "  %s, started %s, duration %s", d.Status, d.Started.Local().Format(time.DateTime), duration(d.Run))
	if d.Trigger != "" {
		_, _ = fmt.Fprintf(out, ", trigger %s", d.Trigger)
	}
	if d.Environment != "" {
		_, _ = fmt.Fprintf(out, ", environment %s", d.Environment)
	}
	_, _ = fmt.Fprintln(out)
	if d.Error != "" {
		_, _ = fmt.Fprintf(out, "  error: %s\n", d.Error)
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "\nSTEP\tTYPE\tSTATUS\tDURATION")
	for _, a := range d.Actions {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.ID, a.Type, a.Status, runs.Round(time.Duration(a.DurationMs)*time.Millisecond))
	}
	_ = w.Flush()
	_, _ = fmt.Fprintln(out, "\nLog:")
	if d.LogTruncated {
		_, _ = fmt.Fprintf(out, "  (older lines dropped, %d kept)\n", len(d.Log))
	}
	for _, l := range d.Log {
		prefix := ""
		if l.ActionID != "" {
			prefix = "[" + l.ActionID + "] "
		}
		_, _ = fmt.Fprintf(out, "  %s %-5s %s%s\n", l.Time.Local().Format("15:04:05.000"), levelLabel(l.Level), prefix, l.Message)
	}
}

func levelLabel(l string) string {
	switch l {
	case "error":
		return "ERROR"
	case "warn":
		return "WARN"
	case "debug":
		return "DEBUG"
	}
	return "INFO"
}
