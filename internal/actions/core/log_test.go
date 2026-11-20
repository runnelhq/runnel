package core

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

func TestLogConfig(t *testing.T) {
	actiontest.ConfigError(t, logDefinition, nil, `missing required field "message"`)
	actiontest.ConfigError(t, logDefinition, map[string]any{"message": "hi", "colour": "red"}, `unknown field "colour"`)
	actiontest.ConfigError(t, logDefinition, map[string]any{"message": []any{"a"}}, `field "message": expected string, got array`)
}

func TestLogLevels(t *testing.T) {
	for level, want := range map[string]slog.Level{"": slog.LevelInfo, "debug": slog.LevelDebug, "WARN": slog.LevelWarn, "error": slog.LevelError} {
		a := actiontest.New(t, logDefinition, map[string]any{"message": "hello", "level": level})
		_, logs, err := actiontest.Execute(context.Background(), a)
		if err != nil || len(logs) != 1 || logs[0] != (actiontest.Logged{Level: want, Msg: "hello"}) {
			t.Errorf("level %q: logs %v, err %v", level, logs, err)
		}
	}

	a := actiontest.New(t, logDefinition, map[string]any{"message": "x", "level": "loud"})
	if _, _, err := actiontest.Execute(context.Background(), a); err == nil || !strings.Contains(err.Error(), `invalid level "loud"`) {
		t.Errorf("invalid level: %v", err)
	}
}

func TestLogTrimsTrailingLineBreak(t *testing.T) {
	a := actiontest.New(t, logDefinition, map[string]any{"message": "block\nscalar\r\n"})
	if _, logs, _ := actiontest.Execute(context.Background(), a); len(logs) != 1 || logs[0].Msg != "block\nscalar" {
		t.Errorf("logs %v", logs)
	}
}
