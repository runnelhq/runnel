package logfile

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// The text format is a line per message as before (S41): time, level, message, then
// the attributes; line breaks never start a new line (no faked log lines).
func TestTextHandler(t *testing.T) {
	var b bytes.Buffer
	l := slog.New(NewHandler(&b, Options{Level: slog.LevelInfo, TimeLayout: "15:04"}))
	l.Debug("hidden")
	l.Info("run succeeded", "run", "01K", "path", "workflows/a b.workflow.yaml", "durationMs", 12)
	l.Warn("line 1\nERROR faked line\r\n", "error", "x\ny")
	l.With("trigger", "cron:nightly").WithGroup("g").Error("boom", "k", "")
	lines := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("%d lines:\n%s", len(lines), b.String())
	}
	for i, want := range []string{
		` INFO  run succeeded run=01K path="workflows/a b.workflow.yaml" durationMs=12`,
		` WARN  line 1\nERROR faked line\r\n error=x\ny`,
		` ERROR boom trigger=cron:nightly g.k=""`,
	} {
		if len(lines[i]) < 5 || lines[i][5:] != want {
			t.Errorf("line %d: %q, want …%q", i, lines[i], want)
		}
	}
}

// The json format is an object per line with time, level, msg and the attributes.
func TestJSONHandler(t *testing.T) {
	var b bytes.Buffer
	l := slog.New(NewHandler(&b, Options{Format: FormatJSON, Level: slog.LevelWarn}))
	l.Info("hidden")
	l.Warn("run failed", "run", "01K", "error", "line 1\nline 2")
	var got map[string]any
	if err := json.Unmarshal(b.Bytes(), &got); err != nil || strings.Count(b.String(), "\n") != 1 {
		t.Fatalf("%v: %s", err, b.String())
	}
	if got["level"] != "WARN" || got["msg"] != "run failed" || got["run"] != "01K" || got["error"] != "line 1\nline 2" || got["time"] == nil {
		t.Errorf("%v", got)
	}
}

func TestParseOptions(t *testing.T) {
	for in, want := range map[string]slog.Level{"": slog.LevelInfo, "DEBUG": slog.LevelDebug, "warning": slog.LevelWarn, "error": slog.LevelError} {
		if got, err := ParseLevel(in); err != nil || got != want {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
	if _, err := ParseLevel("trace"); err == nil || !strings.Contains(err.Error(), "expected debug, info, warn or error") {
		t.Errorf("trace: %v", err)
	}
	if err := CheckFormat("xml"); err == nil || !strings.Contains(err.Error(), "expected text or json") {
		t.Errorf("xml: %v", err)
	}
}
