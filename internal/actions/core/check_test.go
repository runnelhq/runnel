package core

import (
	"context"
	"testing"

	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

func TestCheckConfig(t *testing.T) {
	actiontest.ConfigError(t, checkDefinition, nil, `missing required field "condition"`)
	actiontest.ConfigError(t, checkDefinition, map[string]any{"condition": "yes"}, `condition: expected true or false, got "yes"`)
	actiontest.New(t, checkDefinition, map[string]any{"condition": "{{ x > 1 }}"})
	actiontest.New(t, checkDefinition, map[string]any{"condition": "${CHECK}"})
}

func TestCheck(t *testing.T) {
	for _, tt := range []struct {
		condition, message, err string
		out                     any
	}{
		{"true", "", "", true},
		{" TRUE ", "", "", true},
		{"false", "", "the condition is false", false},
		{"false", "No order files in the inbox\n", "No order files in the inbox", false},
		{"maybe", "", `condition: expected true or false, got "maybe" (write an expression such as {{ x > 0 }})`, nil},
	} {
		// At run time the engine has evaluated the expression; the action gets its text.
		a := actiontest.New(t, checkDefinition, map[string]any{"condition": "{{ x }}", "message": tt.message})
		a.(*checkAction).Condition = tt.condition
		res, _, err := actiontest.Execute(context.Background(), a)
		if got := ""; err != nil {
			got = err.Error()
			if got != tt.err {
				t.Errorf("%q: err %q, want %q", tt.condition, got, tt.err)
			}
		} else if tt.err != "" {
			t.Errorf("%q: no error, want %q", tt.condition, tt.err)
		}
		if res.Output != tt.out {
			t.Errorf("%q: output %v, want %v", tt.condition, res.Output, tt.out)
		}
	}
}
