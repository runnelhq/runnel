package file

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/runnelhq/runnel/internal/action"
)

// file.exists succeeds if a file or folder exists and fails otherwise, so that the
// workflow can branch with success and failure links. Output: the matching paths.
var existsDefinition = action.Definition{
	Type:        "file.exists",
	Category:    action.CategoryFiles,
	Label:       "File Exists",
	Description: "Checks whether files or folders exist; output: the matching paths",
	Config:      exists{},
	New:         newExists,
}

type exists struct {
	Path string `json:"path" doc:"File or folder; wildcards are allowed in the name, e.g. *.csv" schema:"required,example=C:\\in\\*.csv"`
	Kind string `json:"kind" doc:"What has to exist" schema:"enum=any|file|folder,default=any"`
}

func newExists(with map[string]any) (action.Action, error) {
	a := &exists{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *exists) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	found, err := matchKind(a.Path, a.Kind)
	if err != nil {
		return action.Result{}, err
	}
	if len(found) == 0 {
		return action.Result{Output: []any{}}, fmt.Errorf("no %s matches %q", kindName(a.Kind), a.Path)
	}
	ec.Logf(slog.LevelDebug, "found %d %s for %s", len(found), kindNames(a.Kind, len(found)), a.Path)
	return action.Result{Output: list(found)}, nil
}

// matchKind matches a path and keeps only files, folders or both (kind any or "").
func matchKind(path, kind string) ([]string, error) {
	switch kind {
	case "", "any", "file", "folder":
	default:
		return nil, fmt.Errorf("invalid kind %q (expected any, file or folder)", kind)
	}
	all, err := match(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range all {
		info, err := os.Stat(p)
		if err != nil {
			continue // removed in the meantime
		}
		if kind == "file" && info.IsDir() || kind == "folder" && !info.IsDir() {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func kindName(kind string) string {
	switch kind {
	case "file":
		return "file"
	case "folder":
		return "folder"
	}
	return "file or folder"
}

// kindNames is kindName for a number of entries, e.g. "2 files or folders".
func kindNames(kind string, n int) string {
	if n == 1 {
		return kindName(kind)
	}
	if kind == "file" || kind == "folder" {
		return kind + "s"
	}
	return "files or folders"
}
