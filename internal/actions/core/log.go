package core

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
)

// core.log writes a message to the run log.
var logDefinition = action.Definition{
	Type:        "core.log",
	Category:    action.CategoryGeneral,
	Label:       "Log",
	Description: "Writes a message to the run log",
	Example:     "message: \"Imported {{ results.import.output.write.written }} records\"\nlevel: info\n",
	Config:      logAction{},
	New:         newLog,
	Reference:   action.Reference{Errors: "Fails only for an invalid level."},
}

type logAction struct {
	Message string `json:"message" doc:"Text to write; may span several lines" schema:"required,format=runnel-text,example=Import finished"`
	Level   string `json:"level" doc:"Log level of the message" schema:"enum=debug|info|warn|error,default=info"`
}

func newLog(with map[string]any) (action.Action, error) {
	a := &logAction{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *logAction) Execute(_ context.Context, ec *action.ExecContext) (action.Result, error) {
	level, err := parseLevel(a.Level)
	if err != nil {
		return action.Result{}, err
	}
	// Block scalars (message: |) end with a line break; it would show as an empty log line.
	ec.Log(level, strings.TrimRight(a.Message, "\r\n"))
	return action.Result{}, nil
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("invalid level %q (expected debug, info, warn or error)", s)
}
