package file

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/runnelhq/runnel/internal/action"
)

// file.move moves files or folders, with the same rules as file.copy. Within a drive
// it renames; across drives it copies and then deletes the source. Output: the new
// paths (of the moved files and folders).
var moveDefinition = action.Definition{
	Type:        "file.move",
	Category:    action.CategoryFiles,
	Label:       "Move Files",
	Description: "Moves files or folders; output: the new paths",
	Example:     "source: inbox/orders.csv\ntarget: done/\n",
	Config:      transfer{},
	New:         newMove,
	Renamed:     renamedTransfer,
}

type moveAction struct{ transfer }

func newMove(with map[string]any) (action.Action, error) {
	a := &moveAction{}
	if err := action.DecodeConfig(with, &a.transfer); err != nil {
		return nil, err
	}
	return a, a.checkIfExists()
}

func (a *moveAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	pairs, err := a.targets(ctx)
	if err == nil {
		err = a.check(pairs, "move")
	}
	if err != nil {
		return action.Result{}, err
	}
	var moved []string
	for _, p := range pairs {
		if err := ctx.Err(); err != nil {
			return action.Result{Output: list(moved)}, err
		}
		ec.Logf(slog.LevelDebug, "moving %s to %s", p[0], p[1])
		err := movePath(ctx, p[0], p[1], a.IfExists)
		if errors.Is(err, errSkipped) {
			continue
		}
		if err != nil {
			return action.Result{Output: list(moved)}, err
		}
		moved = append(moved, p[1])
	}
	ec.Logf(slog.LevelInfo, "moved %d %s to %s", len(moved), plural(len(moved), "item"), a.Target)
	return action.Result{Output: list(moved)}, nil
}

func movePath(ctx context.Context, src, dst, ifExists string) error {
	if info, err := os.Stat(dst); err == nil {
		switch {
		case info.IsDir():
			return fmt.Errorf("cannot move %q: %q already exists as a folder", src, dst)
		case ifExists == "skip":
			return errSkipped // the source stays where it is
		case ifExists != "replace":
			return fmt.Errorf("cannot move %q: %q already exists (set ifExists: replace or skip)", src, dst)
		}
	}
	if isFolder(src) && within(dst, src) {
		return fmt.Errorf("cannot move %q into itself (%q)", src, dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("cannot create folder %q: %w", filepath.Dir(dst), plain(err))
	}
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("cannot move %q to %q: %w", src, dst, plain(err))
	}
	// Another drive or share: copy, then delete the source.
	if _, err := copyPath(ctx, src, dst, ifExists); err != nil {
		return err
	}
	if err := os.RemoveAll(src); err != nil {
		return fmt.Errorf("copied %q to %q, but cannot delete the source: %w", src, dst, plain(err))
	}
	return nil
}
