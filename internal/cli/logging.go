package cli

import (
	"fmt"
	"io"
	"log/slog"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/logfile"
)

// logOptions are --log-level and --log-format (S71); for the service also the log file,
// its size and the number of old files.
type logOptions struct {
	level, format string
	// Only for the service:
	file    string
	maxSize string
	files   int
}

// logFlags adds --log-level and --log-format.
func logFlags(cmd *cobra.Command, o *logOptions) {
	cmd.Flags().StringVar(&o.level, "log-level", "info", "least level of the messages logged: debug, info, warn or error")
	cmd.Flags().StringVar(&o.format, "log-format", logfile.FormatText, "format of the log: text (a line per message) or json (an object per line, e.g. for Splunk or ELK)")
}

// logFileFlags adds the options of the service's log file.
func logFileFlags(cmd *cobra.Command, o *logOptions, defaultFile string) {
	cmd.Flags().StringVar(&o.file, "log-file", "", "log file of the service; empty: "+defaultFile)
	cmd.Flags().StringVar(&o.maxSize, "log-max-size", "10MB", "size from which the log file is rotated, e.g. 10MB")
	cmd.Flags().IntVar(&o.files, "log-files", logfile.DefaultKeep, "number of old log files kept (<file>.1 …)")
}

// check checks the options and returns the level and the size of the log file.
func (o logOptions) check() (slog.Level, int64, error) {
	level, err := logfile.ParseLevel(o.level)
	if err != nil {
		return 0, 0, fmt.Errorf("--log-level: %w", err)
	}
	if err := logfile.CheckFormat(o.format); err != nil {
		return 0, 0, fmt.Errorf("--log-format: %w", err)
	}
	size, err := format.ParseSize("--log-max-size", o.maxSize, logfile.DefaultMaxSize)
	if err != nil {
		return 0, 0, err
	}
	if size < 1<<10 {
		return 0, 0, fmt.Errorf("--log-max-size: %q is less than 1KB", o.maxSize)
	}
	if o.files < 0 || o.files > 100 {
		return 0, 0, fmt.Errorf("invalid --log-files %d (expected 1–100)", o.files)
	}
	return level, size, nil
}

// serviceArgs returns the options for the command line of the service.
func (o logOptions) serviceArgs(file string) []string {
	var args []string
	for _, kv := range [][2]string{{"--log-level", o.level}, {"--log-format", o.format}, {"--log-max-size", o.maxSize}, {"--log-file", file}} {
		if kv[1] != "" {
			args = append(args, kv[0], kv[1])
		}
	}
	if o.files > 0 {
		args = append(args, "--log-files", strconv.Itoa(o.files))
	}
	return args
}

// newLogger returns the logger of the server log, writing to w.
func newLogger(w io.Writer, o logOptions, timeLayout string) (*slog.Logger, error) {
	level, _, err := o.check()
	if err != nil {
		return nil, err
	}
	return slog.New(logfile.NewHandler(w, logfile.Options{Format: o.format, Level: level, TimeLayout: timeLayout})), nil
}
