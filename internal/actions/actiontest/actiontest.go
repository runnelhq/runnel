// Package actiontest contains helpers for testing actions.
package actiontest

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/action"
)

// Logged is a message an action wrote to the run log.
type Logged struct {
	Level slog.Level
	Msg   string
}

// New creates an action from its definition and fails the test on an error.
func New(t *testing.T, def action.Definition, with map[string]any) action.Action {
	t.Helper()
	a, err := def.New(with)
	if err != nil {
		t.Fatalf("%s: unexpected configuration error: %v", def.Type, err)
	}
	return a
}

// ConfigError expects def.New to fail with an error containing want.
func ConfigError(t *testing.T, def action.Definition, with map[string]any, want string) {
	t.Helper()
	if _, err := def.New(with); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("%s %v: error %v, want containing %q", def.Type, with, err, want)
	}
}

// Execute runs an action with the run id "run" and the action id "act" and
// returns its result, its log messages and its error.
func Execute(ctx context.Context, a action.Action) (action.Result, []Logged, error) {
	var logs []Logged
	ec := &action.ExecContext{RunID: "run", ActionID: "act", Log: func(l slog.Level, m string) {
		logs = append(logs, Logged{l, m})
	}}
	res, err := a.Execute(ctx, ec)
	return res, logs, err
}
