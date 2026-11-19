package core

import (
	"context"
	"testing"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

func TestDummyConfig(t *testing.T) {
	actiontest.ConfigError(t, dummyDefinition, map[string]any{"message": "x"}, "core.dummy has no configuration")
}

func TestDummy(t *testing.T) {
	res, logs, err := actiontest.Execute(context.Background(), actiontest.New(t, dummyDefinition, nil))
	if err != nil || res != (action.Result{}) || len(logs) != 0 {
		t.Errorf("result %+v, logs %v, err %v", res, logs, err)
	}
}
