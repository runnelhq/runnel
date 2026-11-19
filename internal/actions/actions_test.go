package actions

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/model"
)

// TestContract checks rules that every built-in action must follow (ADR 0013).
// It runs automatically for every action added to a namespace.
func TestContract(t *testing.T) {
	r := Builtin()
	if len(r.Types()) == 0 {
		t.Fatal("no built-in actions")
	}
	for _, typ := range r.Types() {
		def, _ := r.Lookup(typ)
		t.Run(typ, func(t *testing.T) {
			if !model.ValidTypeName(typ) {
				t.Errorf("invalid type name")
			}
			if ns := model.TypeNamespace(typ); !model.IsReservedNamespace(ns) {
				t.Errorf("namespace %q of a built-in action must be reserved (internal/model/namespaces.go)", ns)
			}
			if def.Description == "" {
				t.Error("missing description")
			}
			if def.New == nil {
				t.Fatal("missing constructor")
			}
			// Unknown fields must be rejected, so that typos in workflows are reported.
			if _, err := def.New(map[string]any{"no-such-field": true}); err == nil {
				t.Error("unknown field in 'with' was accepted")
			}
		})
	}
}

// TestContractCancellation checks that no built-in action keeps running after the
// run was cancelled. Actions whose configuration is invalid without fields are skipped.
func TestContractCancellation(t *testing.T) {
	r := Builtin()
	samples := map[string]map[string]any{
		"core.log":  {"message": "x"},
		"core.wait": {"duration": "1h"},
	}
	for _, typ := range r.Types() {
		def, _ := r.Lookup(typ)
		a, err := def.New(samples[typ])
		if err != nil {
			t.Errorf("%s: add a sample configuration to this test: %v", typ, err)
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		done := make(chan error, 1)
		go func() {
			_, err := a.Execute(ctx, &action.ExecContext{RunID: "run", ActionID: "act", Log: func(slog.Level, string) {}})
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("%s: cancelled run returned %v, want nil or context.Canceled", typ, err)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("%s: still running 2s after the run was cancelled", typ)
		}
	}
}
