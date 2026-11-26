package file

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/paths"
)

// file.wait waits until files exist (e.g. a delivery from another system) and,
// optionally, until their size has stopped changing, so that half-written files are
// not picked up. Output: the matching paths.
var waitDefinition = action.Definition{
	Type:        "file.wait",
	Category:    action.CategoryFiles,
	Label:       "Wait for File",
	Description: "Waits until files exist; output: the matching paths",
	Example:     "path: inbox/*.csv\nmaxWait: 30m\nstableFor: 10s\n",
	Config:      wait{},
	New:         newWait,
	Reference: action.Reference{
		Outputs: []action.OutputField{{Name: "output", Type: "list", Description: "Paths of the matching files, sorted"}},
		Errors:  "Fails when no file matches after maxWait, or the files still change in size after maxWait (stableFor), and when the folder cannot be read.",
		Limits:  []string{"Looks every interval (default 5s, at least 10ms); maxWait limits the wait (default 1h; 0: until the timeout of the action or the end of the run)."},
	},
}

type wait struct {
	Path      string `json:"path" doc:"File to wait for, relative to the project folder or absolute; wildcards are allowed in the name, e.g. *.csv" schema:"required,example=C:\\in\\*.csv"`
	Interval  string `json:"interval" doc:"How often to look, e.g. 5s or 1m" schema:"format=runnel-duration,default=5s"`
	MaxWait   string `json:"maxWait" doc:"Give up and fail after this time; 0: wait until the action's timeout or the end of the run" schema:"format=runnel-duration,default=1h"`
	StableFor string `json:"stableFor" doc:"The size of the files must not change for this time, e.g. 10s; empty: do not check" schema:"format=runnel-duration,example=10s"`
}

func newWait(with map[string]any) (action.Action, error) {
	a := &wait{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *wait) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	interval, err := duration("interval", a.Interval, 5*time.Second)
	if err != nil {
		return action.Result{}, err
	}
	if interval < 10*time.Millisecond {
		return action.Result{}, fmt.Errorf("interval %s is too short (at least 10ms)", interval)
	}
	maxWait, err := MaxWait(a.MaxWait)
	if err != nil {
		return action.Result{}, err
	}
	stableFor, err := duration("stableFor", a.StableFor, 0)
	if err != nil {
		return action.Result{}, err
	}

	start := time.Now()
	var sizes map[string]int64 // sizes at the last check, for stableFor
	var stableSince time.Time
	ec.Logf(slog.LevelDebug, "waiting for %s", a.Path)
	for {
		found, err := matchKind(paths.Resolve(ctx, a.Path), "file")
		if err != nil {
			return action.Result{}, err
		}
		if len(found) > 0 {
			if stableFor <= 0 {
				return action.Result{Output: list(found)}, nil
			}
			now := sizesOf(found)
			if !equalSizes(now, sizes) {
				sizes, stableSince = now, time.Now()
			} else if time.Since(stableSince) >= stableFor {
				return action.Result{Output: list(found)}, nil
			}
		}
		if maxWait > 0 && time.Since(start) >= maxWait {
			if len(found) > 0 {
				return action.Result{}, fmt.Errorf("files matching %q still changed after %s", a.Path, maxWait)
			}
			return action.Result{}, fmt.Errorf("no file matching %q after %s", a.Path, maxWait)
		}
		t := time.NewTimer(interval)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return action.Result{}, ctx.Err()
		}
	}
}

// DefaultMaxWait is how long file.wait and transfer.wait wait without maxWait (R5b): a
// forgotten value does not block a run for ever.
const DefaultMaxWait = time.Hour

// MaxWait reads maxWait of file.wait and transfer.wait: empty is DefaultMaxWait, 0 no
// limit of its own.
func MaxWait(text string) (time.Duration, error) {
	return duration("maxWait", strings.TrimSpace(text), DefaultMaxWait)
}

// duration reads an optional duration field.
func duration(field, text string, fallback time.Duration) (time.Duration, error) {
	if text == "" {
		return fallback, nil
	}
	d, err := model.Duration(text).Value()
	if err != nil {
		return 0, fmt.Errorf("%s: %w", field, err)
	}
	return d, nil
}

func sizesOf(paths []string) map[string]int64 {
	m := make(map[string]int64, len(paths))
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil {
			m[p] = info.Size()
		}
	}
	return m
}

func equalSizes(a, b map[string]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}
