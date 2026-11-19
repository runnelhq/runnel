package core

import (
	"context"
	"testing"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

func TestSuccess(t *testing.T) {
	actiontest.ConfigError(t, successDefinition, map[string]any{"msg": "x"}, `unknown field "msg"`)

	a := actiontest.New(t, successDefinition, map[string]any{"message": "done"})
	if res, _, err := actiontest.Execute(context.Background(), a); err != nil || res.End != action.EndSuccess || res.Message != "done" {
		t.Errorf("result %+v, err %v", res, err)
	}
}

func TestAbort(t *testing.T) {
	a := actiontest.New(t, abortDefinition, nil)
	if res, _, err := actiontest.Execute(context.Background(), a); err != nil || res.End != action.EndFailure || res.Message != `aborted by action "act"` {
		t.Errorf("default message: result %+v, err %v", res, err)
	}

	a = actiontest.New(t, abortDefinition, map[string]any{"message": "input missing"})
	if res, _, _ := actiontest.Execute(context.Background(), a); res.Message != "input missing" {
		t.Errorf("message %q", res.Message)
	}
}
