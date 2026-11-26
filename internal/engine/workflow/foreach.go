package workflow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/vars"
)

// executeForEach runs an action for each entry of its forEach list (roadmap S24b): the
// entry is row in its expressions, up to parallel entries run at the same time, each
// with the retries and timeout of the action. The output lists the entries with their
// result (items) and counts them (succeeded, failed); the log messages of an entry start
// with its number, e.g. "[2/5] ". By default the first failed entry stops the others
// and fails the action; with continueOnEntryError all entries run and the action fails
// at the end if one of them failed.
func (r *Runner) executeForEach(ctx context.Context, runID string, a *model.Action, scope *vars.Scope, values map[string]any) (action.Result, error) {
	v, err := r.evalText(a.ForEach, scope, values)
	if err != nil {
		return action.Result{}, fmt.Errorf("forEach: %w", err)
	}
	var list []any
	switch v := v.(type) {
	case []any:
		list = v
	case nil:
	default:
		return action.Result{}, fmt.Errorf("forEach: the expression must return a list, got %s", expr.Format(v))
	}
	stop := !a.ContinueOnEntryError
	parallel := min(max(a.Parallel, 1), max(len(list), 1))

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	errStopped := errors.New("stopped after a failed entry")

	type outcome struct {
		done bool
		res  action.Result
		err  error
	}
	outcomes := make([]outcome, len(list))
	var mu sync.Mutex
	first := -1 // index of the first failed entry, by the time it failed
	next := 0
	var wg sync.WaitGroup
	for range parallel {
		wg.Go(func() {
			for {
				mu.Lock()
				i := next
				next++
				mu.Unlock()
				if i >= len(list) || ctx.Err() != nil {
					return
				}
				vals := maps.Clone(values)
				vals["row"] = list[i]
				res, err := r.executeWithRetry(ctx, runID, a, scope, vals, fmt.Sprintf("[%d/%d] ", i+1, len(list)))
				if err != nil && errors.Is(context.Cause(ctx), errStopped) {
					continue // stopped because another entry failed: not counted
				}
				mu.Lock()
				outcomes[i] = outcome{done: true, res: res, err: err}
				if err != nil && first < 0 {
					first = i
				}
				mu.Unlock()
				if err != nil {
					if stop {
						cancel(errStopped)
					} else {
						r.log(runID, a, slog.LevelWarn, fmt.Sprintf("entry %d of %d failed: %v", i+1, len(list), err))
					}
				}
			}
		})
	}
	wg.Wait()

	items := []any{}
	var succeeded, failed int64
	for i, o := range outcomes {
		if !o.done {
			continue
		}
		item := map[string]any{"row": list[i], "success": o.err == nil, "output": o.res.Output, "error": ""}
		if o.err != nil {
			item["error"] = o.err.Error()
			failed++
		} else {
			succeeded++
		}
		items = append(items, item)
	}
	res := action.Result{Output: map[string]any{"items": items, "succeeded": succeeded, "failed": failed}}
	r.log(runID, a, slog.LevelDebug, fmt.Sprintf("%d of %d entries succeeded", succeeded, len(list)))
	if err := context.Cause(ctx); err != nil && !errors.Is(err, errStopped) {
		return res, err // the run was cancelled or the action timed out
	}
	if first >= 0 {
		if stop {
			return res, fmt.Errorf("entry %d of %d: %w", first+1, len(list), outcomes[first].err)
		}
		return res, fmt.Errorf("%d of %d entries failed; first: entry %d: %w", failed, len(list), first+1, outcomes[first].err)
	}
	return res, nil
}
