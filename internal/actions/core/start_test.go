package core

import (
	"context"
	"testing"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

func TestStart(t *testing.T) {
	actiontest.ConfigError(t, startDefinition, map[string]any{"x": 1}, "core.start has no configuration")

	res, logs, err := actiontest.Execute(context.Background(), actiontest.New(t, startDefinition, nil))
	if err != nil || res != (action.Result{}) || len(logs) != 0 {
		t.Errorf("result %+v, logs %v, err %v", res, logs, err)
	}
}
