package file

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/paths"
)

// file.delete deletes files and, with recursive, folders with their content.
// Nothing to delete is fine unless ifMissing: fail is set. Output: the deleted paths.
var deleteDefinition = action.Definition{
	Type:        "file.delete",
	Category:    action.CategoryFiles,
	Label:       "Delete Files",
	Description: "Deletes files or folders; output: the deleted paths",
	Example:     "path: out/*.tmp\n",
	Config:      deleteAction{},
	New:         newDelete,
	Reference: action.Reference{
		Outputs: []action.OutputField{{Name: "output", Type: "list", Description: "Paths of the deleted files and folders"}},
		Errors:  "Fails before deleting anything when a match is a folder and recursive is not set. Nothing to delete is no error, except with ifMissing: fail.",
	},
}

type deleteAction struct {
	Path      string `json:"path" doc:"File or folder, relative to the project folder or absolute; wildcards are allowed in the name, e.g. *.tmp" schema:"required,example=C:\\work\\*.tmp"`
	Recursive bool   `json:"recursive" doc:"Also delete folders with everything in them" schema:"default=false"`
	IfMissing string `json:"ifMissing" doc:"If nothing matches the path: skip (nothing is deleted) or fail" schema:"enum=skip|fail,default=skip"`
}

func newDelete(with map[string]any) (action.Action, error) {
	a := &deleteAction{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	switch a.IfMissing {
	case "", "skip", "fail":
	default:
		if !strings.Contains(a.IfMissing, "${") {
			return nil, fmt.Errorf("invalid ifMissing %q (expected skip or fail)", a.IfMissing)
		}
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
		if a.IfMissing == "fail" {
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
