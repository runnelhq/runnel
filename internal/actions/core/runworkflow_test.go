package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

func TestRunWorkflowConfig(t *testing.T) {
	actiontest.ConfigError(t, runWorkflowDefinition, nil, `missing required field "workflow"`)
	actiontest.ConfigError(t, runWorkflowDefinition, map[string]any{"workflow": "a.workflow.yaml", "parameters": map[string]any{}}, `unknown field "parameters"`)
	actiontest.ConfigError(t, runWorkflowDefinition, map[string]any{"workflow": "a.workflow.yaml", "params": "X=1"}, `field "params"`)
}

func TestRunWorkflowPassesParamsAndReturnsOutputs(t *testing.T) {
	a := actiontest.New(t, runWorkflowDefinition, map[string]any{
		"workflow": "workflows/child.workflow.yaml",
		"params": map[string]any{
			"NAME": "Ada", "COUNT": 3, "RATE": 1.5, "ON": true,
			"DAY": time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC), "LIST": []any{"a", "b"},
		},
	})
	var gotPath string
	var gotParams map[string]string
	ec := &action.ExecContext{RunWorkflow: func(_ context.Context, path string, params map[string]string) (map[string]any, error) {
		gotPath, gotParams = path, params
		return map[string]any{"ROWS": 42}, nil
	}}
	res, err := a.Execute(context.Background(), ec)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "workflows/child.workflow.yaml" {
		t.Errorf("path %q", gotPath)
	}
	want := map[string]string{"NAME": "Ada", "COUNT": "3", "RATE": "1.5", "ON": "true", "DAY": "2026-11-20T00:00:00Z", "LIST": `["a","b"]`}
	for k, v := range want {
		if gotParams[k] != v {
			t.Errorf("param %s = %q, want %q", k, gotParams[k], v)
		}
	}
	if out, ok := res.Output.(map[string]any); !ok || out["ROWS"] != 42 {
		t.Errorf("output %#v", res.Output)
	}

	// Without outputs, the output is an empty map (so that expressions can test it).
	ec.RunWorkflow = func(context.Context, string, map[string]string) (map[string]any, error) { return nil, nil }
	if res, _ := a.Execute(context.Background(), ec); res.Output == nil {
		t.Error("output is nil")
	}
}

func TestRunWorkflowErrors(t *testing.T) {
	a := actiontest.New(t, runWorkflowDefinition, map[string]any{"workflow": "child.workflow.yaml"})
	if _, _, err := actiontest.Execute(context.Background(), a); err == nil || !strings.Contains(err.Error(), "not available here") {
		t.Errorf("without runner: %v", err)
	}
	failed := errors.New(`workflow child.workflow.yaml failed: boom`)
	ec := &action.ExecContext{RunWorkflow: func(context.Context, string, map[string]string) (map[string]any, error) { return nil, failed }}
	if _, err := a.Execute(context.Background(), ec); !errors.Is(err, failed) {
		t.Errorf("failure: %v", err)
	}
	empty := actiontest.New(t, runWorkflowDefinition, map[string]any{"workflow": ""})
	if _, err := empty.Execute(context.Background(), ec); err == nil || !strings.Contains(err.Error(), "no workflow given") {
		t.Errorf("empty: %v", err)
	}
	// A cancelled run is reported as cancellation, not as failure of the workflow.
	ctx, cancel := context.WithCancel(context.Background())
	ec.RunWorkflow = func(context.Context, string, map[string]string) (map[string]any, error) {
		cancel()
		return nil, errors.New("workflow child.workflow.yaml failed: run cancelled")
	}
	if _, err := a.Execute(ctx, ec); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}
