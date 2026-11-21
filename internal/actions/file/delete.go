package file

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/paths"
)

// file.delete deletes files and, with recursive, folders with their content.
// Nothing to delete is fine unless mustExist is set. Output: the deleted paths.
var deleteDefinition = action.Definition{
	Type:        "file.delete",
	Category:    action.CategoryFiles,
	Label:       "Delete Files",
	Description: "Deletes files or folders; output: the deleted paths",
	Config:      deleteAction{},
	New:         newDelete,
}

type deleteAction struct {
	Path      string `json:"path" doc:"File or folder, relative to the project folder or absolute; wildcards are allowed in the name, e.g. *.tmp" schema:"required,example=C:\\work\\*.tmp"`
	Recursive bool   `json:"recursive" doc:"Also delete folders with everything in them" schema:"default=false"`
	MustExist bool   `json:"mustExist" doc:"Fail if nothing matches the path" schema:"default=false"`
}

func newDelete(with map[string]any) (action.Action, error) {
	a := &deleteAction{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *deleteAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	found, err := match(paths.Resolve(ctx, a.Path))
	if err != nil {
		return action.Result{}, err
	}
	if len(found) == 0 {
		if a.MustExist {
			return action.Result{Output: []any{}}, fmt.Errorf("no file or folder matches %q", a.Path)
		}
		ec.Logf(slog.LevelDebug, "nothing to delete for %s", a.Path)
		return action.Result{Output: []any{}}, nil
	}
	// Folders are checked first, so that nothing is deleted if one stops the action.
	for _, p := range found {
		if isFolder(p) && !a.Recursive {
			return action.Result{Output: []any{}}, fmt.Errorf("cannot delete %q: it is a folder (set recursive: true to delete it with its content)", p)
		}
	}
	var deleted []string
	for _, p := range found {
		if err := ctx.Err(); err != nil {
			return action.Result{Output: list(deleted)}, err
		}
		if err := os.RemoveAll(p); err != nil {
			return action.Result{Output: list(deleted)}, fmt.Errorf("cannot delete %q: %w", p, plain(err))
		}
		deleted = append(deleted, p)
	}
	ec.Logf(slog.LevelInfo, "deleted %d %s", len(deleted), plural(len(deleted), "item"))
	return action.Result{Output: list(deleted)}, nil
}
