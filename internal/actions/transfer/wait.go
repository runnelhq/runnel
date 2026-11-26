package transfer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/pkg/sftp"

	"github.com/runnelhq/runnel/internal/action"
)

// transfer.wait waits until files exist on the server of a connection and, optionally,
// until their size has stopped changing, like file.wait. It looks every interval over one
// SSH connection; if the connection drops (or a look takes longer than timeout), it
// connects again at the next look. A refused host key or sign-in ends the wait. Output: the
// matching paths.
var waitDefinition = action.Definition{
	Type:        "transfer.wait",
	Category:    action.CategoryNetwork,
	Label:       "Wait for File on Server",
	Description: "Waits until files exist on a server (SFTP); output: the matching paths",
	Example:     "connection: partner-sftp\npath: /outgoing/*.csv\ninterval: 1m\nmaxWait: 2h\nstableFor: 30s\n",
	Config:      waitAction{},
	New:         newWait,
}

type waitAction struct {
	Connection string `json:"connection" doc:"Connection of the server (a file in connections/)" schema:"required,connection=sftp,example=partner-sftp"`
	Path       string `json:"path" doc:"File to wait for on the server, e.g. /outgoing/*.csv (relative: to the folder after the sign-in); wildcards are allowed in the name and match case-sensitively" schema:"required,example=/outgoing/*.csv"`
	Interval   string `json:"interval" doc:"How often to look, e.g. 5s or 1m" schema:"format=runnel-duration,default=5s"`
	MaxWait    string `json:"maxWait" doc:"Give up and fail after this time; empty: wait until the action's timeout or the end of the run" schema:"format=runnel-duration,example=2h"`
	StableFor  string `json:"stableFor" doc:"The size of the files must not change for this time, e.g. 30s; empty: do not check" schema:"format=runnel-duration,example=30s"`
	Timeout    string `json:"timeout" doc:"Longest time for one look on the server, e.g. 30s; after it Runnel connects again" schema:"format=runnel-duration,default=1m"`
}

func newWait(with map[string]any) (action.Action, error) {
	a := &waitAction{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *waitAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	interval, err := durationOf("interval", a.Interval, 5*time.Second)
	if err != nil {
		return action.Result{}, err
	}
	if interval < 10*time.Millisecond {
		return action.Result{}, fmt.Errorf("interval %s is too short (at least 10ms)", interval)
	}
	maxWait, err := durationOf("maxWait", a.MaxWait, 0)
	if err != nil {
		return action.Result{}, err
	}
	stableFor, err := durationOf("stableFor", a.StableFor, 0)
	if err != nil {
		return action.Result{}, err
	}
	timeout, err := durationOf("timeout", a.Timeout, time.Minute)
	if err != nil {
		return action.Result{}, err
	}
	s, err := open(ctx, ec, a.Connection)
	if err != nil {
		return action.Result{}, err
	}
	defer func() {
		if s != nil {
			s.close()
		}
	}()

	start := time.Now()
	var sizes map[string]int64 // sizes at the last look, for stableFor
	var stableSince time.Time
	ec.Logf(slog.LevelDebug, "waiting for %s on %s", a.Path, s.addr)
	var lost error // why the last look failed while the connection is gone
	for {
		var found map[string]int64
		if s == nil {
			if s, err = open(ctx, ec, a.Connection); err != nil {
				if ctx.Err() != nil || refused(err) {
					return action.Result{}, err
				}
				ec.Logf(slog.LevelDebug, "cannot connect again: %v", err)
				s, lost = nil, err
			} else {
				ec.Logf(slog.LevelInfo, "connected again to %s", s.addr)
			}
		}
		if s != nil {
			err = s.within(ctx, timeout, "looking for "+a.Path, func() (err error) {
				found, err = s.files(a.Path)
				return err
			})
			switch {
			case ctx.Err() != nil:
				return action.Result{}, ctx.Err()
			case err != nil && (s.closed() || errors.Is(err, sftp.ErrSSHFxConnectionLost)):
				// The connection dropped or the look took too long: connect again next time.
				ec.Logf(slog.LevelWarn, "lost the connection to %s while waiting for %s: %v", s.addr, a.Path, err)
				s.close()
				s, lost = nil, err
			case err != nil:
				return action.Result{}, err
			default:
				lost = nil
			}
		}
		if len(found) > 0 {
			if stableFor <= 0 {
				return action.Result{Output: list(slices.Sorted(maps.Keys(found)))}, nil
			}
			if !maps.Equal(found, sizes) {
				sizes, stableSince = found, time.Now()
			} else if time.Since(stableSince) >= stableFor {
				return action.Result{Output: list(slices.Sorted(maps.Keys(found)))}, nil
			}
		}
		if maxWait > 0 && time.Since(start) >= maxWait {
			if len(found) > 0 {
				return action.Result{}, fmt.Errorf("files matching %q on the server still changed after %s", a.Path, maxWait)
			}
			if lost != nil {
				return action.Result{}, fmt.Errorf("no file matching %q on the server after %s; the last look failed: %w", a.Path, maxWait, lost)
			}
			return action.Result{}, fmt.Errorf("no file matching %q on the server after %s", a.Path, maxWait)
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

// files returns the files a path on the server stands for with their sizes.
func (s *session) files(p string) (map[string]int64, error) {
	found, err := s.find(p, true, maxEntries)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(found))
	for _, e := range found {
		if !e.info.IsDir() {
			out[e.path] = e.info.Size()
		}
	}
	return out, nil
}
