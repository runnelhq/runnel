package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
)

// core.log writes a message to the run log.
var logDefinition = action.Definition{
	Type:        "core.log",
	Description: "Writes a message to the run log",
	New:         newLog,
}

type logAction struct {
	Message string `json:"message"`
	Level   string `json:"level"`
}

func newLog(with map[string]any) (action.Action, error) {
	a := &logAction{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	if _, ok := with["message"]; !ok {
		return nil, errors.New(`missing required field "message"`)
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
