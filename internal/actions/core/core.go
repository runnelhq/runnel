// Package core contains the basic workflow actions: core.start, core.success,
// core.abort, core.log and core.wait.
package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/model"
)

// Register adds the core actions to a registry.
func Register(r *action.Registry) {
	r.Register(action.Definition{
		Type:        "core.start",
		Description: "Entry point of the workflow",
		New:         newStart,
	})
	r.Register(action.Definition{
		Type:        "core.success",
		Description: "Ends the workflow successfully",
		New:         newEnd(action.EndSuccess),
	})
	r.Register(action.Definition{
		Type:        "core.abort",
		Description: "Ends the workflow as failed",
		New:         newEnd(action.EndFailure),
	})
	r.Register(action.Definition{
		Type:        "core.log",
		Description: "Writes a message to the run log",
		New:         newLog,
	})
	r.Register(action.Definition{
		Type:        "core.wait",
		Description: "Waits for a given time",
		New:         newWait,
	})
}

// --- core.start ---

type start struct{}

func newStart(with map[string]any) (action.Action, error) {
	if len(with) > 0 {
		return nil, errors.New("core.start has no configuration")
	}
	return start{}, nil
}

func (start) Execute(context.Context, *action.ExecContext) (action.Result, error) {
	return action.Result{}, nil
}

// --- core.success / core.abort ---

type end struct {
	kind    action.End
	Message string `json:"message"`
}

func newEnd(kind action.End) func(map[string]any) (action.Action, error) {
	return func(with map[string]any) (action.Action, error) {
		a := &end{kind: kind}
		return a, action.DecodeConfig(with, a)
	}
}

func (a *end) Execute(_ context.Context, ec *action.ExecContext) (action.Result, error) {
	msg := a.Message
	if msg == "" && a.kind == action.EndFailure {
		msg = fmt.Sprintf("aborted by action %q", ec.ActionID)
	}
	return action.Result{End: a.kind, Message: msg}, nil
}

// --- core.log ---

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

// --- core.wait ---

type wait struct {
	Duration string `json:"duration"`
}

func newWait(with map[string]any) (action.Action, error) {
	a := &wait{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	if a.Duration == "" {
		return nil, errors.New(`missing required field "duration"`)
	}
	return a, nil
}

func (a *wait) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	d, err := model.Duration(a.Duration).Value()
	if err != nil {
		return action.Result{}, err
	}
	ec.Logf(slog.LevelDebug, "waiting %s", d)
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return action.Result{}, nil
	case <-ctx.Done():
		return action.Result{}, ctx.Err()
	}
}
