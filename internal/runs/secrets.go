package runs

import (
	"fmt"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/vars"
)

// secretScope gives a run its own mask (ADR 0022): every secret the run reads is added
// to it, and the engines hide those values in events, errors and results.
func secretScope(scope *vars.Scope) (*vars.Scope, *vars.Mask) {
	mask := &vars.Mask{}
	scope.LookupSecret = mask.Lookup(scope.LookupSecret)
	return scope, mask
}

// resolveSecrets reads the secrets of the actions before the run starts: a missing one
// stops the run before it begins (with the place, never a value), and every value is in
// the mask before the first event. Fields for secrets hold ${…} only, no expressions,
// so their values are the same for every execution of the action.
func resolveSecrets(w *model.Workflow, scope *vars.Scope, reg *action.Registry) model.Errors {
	isSecret := ActionSecretField(reg)
	var errs model.Errors
	for i, a := range w.Actions {
		for field, v := range a.With {
			text, ok := v.(string)
			if !ok || !isSecret(a.Type, field) {
				continue
			}
			if _, err := scope.ExpandSecret(text); err != nil {
				errs = append(errs, w.ErrorAt(fmt.Sprintf("$.actions[%d].with.%s", i, field), "action %q: %s: %v", a.ID, field, err))
			}
		}
	}
	errs.Sort()
	return errs
}
