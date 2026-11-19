package core

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/action"
)

func newAction(t *testing.T, typ string, with map[string]any) (action.Action, error) {
	t.Helper()
	r := action.NewRegistry()
	Register(r)
	def, ok := r.Lookup(typ)
	if !ok {
		t.Fatalf("type %s not registered", typ)
	}
	return def.New(with)
}

type logged struct {
	level slog.Level
	msg   string
}

func execute(ctx context.Context, t *testing.T, a action.Action) (action.Result, []logged, error) {
	t.Helper()
	var logs []logged
	ec := &action.ExecContext{RunID: "run", ActionID: "act", Log: func(l slog.Level, m string) {
		logs = append(logs, logged{l, m})
	}}
	res, err := a.Execute(ctx, ec)
	return res, logs, err
}

func TestConfigErrors(t *testing.T) {
	tests := []struct {
		typ  string
		with map[string]any
		want string
	}{
		{"core.start", map[string]any{"x": 1}, "core.start has no configuration"},
		{"core.log", nil, `missing required field "message"`},
		{"core.log", map[string]any{"message": "hi", "colour": "red"}, `unknown field "colour"`},
		{"core.log", map[string]any{"message": []any{"a"}}, `field "message": expected string, got array`},
		{"core.wait", nil, `missing required field "duration"`},
		{"core.success", map[string]any{"msg": "x"}, `unknown field "msg"`},
	}
	for _, tt := range tests {
		if _, err := newAction(t, tt.typ, tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s %v: error %v, want containing %q", tt.typ, tt.with, err, tt.want)
		}
	}
}

func TestLog(t *testing.T) {
	for level, want := range map[string]slog.Level{"": slog.LevelInfo, "debug": slog.LevelDebug, "WARN": slog.LevelWarn, "error": slog.LevelError} {
		a, err := newAction(t, "core.log", map[string]any{"message": "hello", "level": level})
		if err != nil {
			t.Fatal(err)
		}
		_, logs, err := execute(context.Background(), t, a)
		if err != nil || len(logs) != 1 || logs[0] != (logged{want, "hello"}) {
			t.Errorf("level %q: logs %v, err %v", level, logs, err)
		}
	}
	a, _ := newAction(t, "core.log", map[string]any{"message": "block\nscalar\r\n"})
	if _, logs, _ := execute(context.Background(), t, a); len(logs) != 1 || logs[0].msg != "block\nscalar" {
		t.Errorf("trailing line break not removed: %v", logs)
	}
	a, _ = newAction(t, "core.log", map[string]any{"message": "x", "level": "loud"})
	if _, _, err := execute(context.Background(), t, a); err == nil || !strings.Contains(err.Error(), `invalid level "loud"`) {
		t.Errorf("invalid level: %v", err)
	}
}

func TestEndActions(t *testing.T) {
	a, _ := newAction(t, "core.success", map[string]any{"message": "done"})
	if res, _, err := execute(context.Background(), t, a); err != nil || res.End != action.EndSuccess || res.Message != "done" {
		t.Errorf("core.success: %+v %v", res, err)
	}
	a, _ = newAction(t, "core.abort", nil)
	if res, _, err := execute(context.Background(), t, a); err != nil || res.End != action.EndFailure || res.Message != `aborted by action "act"` {
		t.Errorf("core.abort: %+v %v", res, err)
	}
}

func TestWait(t *testing.T) {
	a, _ := newAction(t, "core.wait", map[string]any{"duration": "20ms"})
	start := time.Now()
	if _, _, err := execute(context.Background(), t, a); err != nil || time.Since(start) < 20*time.Millisecond {
		t.Errorf("wait returned after %v, err %v", time.Since(start), err)
	}

	a, _ = newAction(t, "core.wait", map[string]any{"duration": "1h"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := execute(ctx, t, a); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled wait: %v", err)
	}

	a, _ = newAction(t, "core.wait", map[string]any{"duration": "soon"})
	if _, _, err := execute(context.Background(), t, a); err == nil || !strings.Contains(err.Error(), "invalid duration") {
		t.Errorf("invalid duration: %v", err)
	}
}
