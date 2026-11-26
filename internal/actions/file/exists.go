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

// file.exists succeeds if a file or folder exists and fails otherwise, so that the
// workflow can branch with success and failure links. Output: the matching paths.
var existsDefinition = action.Definition{
	Type:        "file.exists",
	Category:    action.CategoryFiles,
	Label:       "File Exists",
	Description: "Checks whether files or folders exist; output: the matching paths",
	Example:     "path: inbox/orders-*.csv\ninclude: files\n",
	Config:      exists{},
	New:         newExists,
	Reference: action.Reference{
		Outputs: []action.OutputField{{Name: "output", Type: "list", Description: "Paths of the matching files or folders, sorted"}},
		Errors:  "Fails when nothing of the kind matches the path, so that success and failure links can branch, and when the folder cannot be read.",
	},
}

type exists struct {
	Path string `json:"path" doc:"File or folder, relative to the project folder or absolute; wildcards are allowed in the name, e.g. *.csv" schema:"required,example=C:\\in\\*.csv"`
	// Include is called as in file.list (R5b, before: kind any|file|folder).
	Include string `json:"include" doc:"What has to exist: files, folders or all (both)" schema:"enum=all|files|folders,default=all"`
}

// includeKinds maps the values of include to the kinds of matchKind.
var includeKinds = map[string]string{"": "any", "all": "any", "files": "file", "folders": "folder"}

// kindOf returns the kind of matchKind for a value of include.
func kindOf(include string) (string, error) {
	kind, ok := includeKinds[include]
	if !ok {
		return "", fmt.Errorf("invalid include %q (expected all, files or folders)", include)
	}
	return kind, nil
}

func newExists(with map[string]any) (action.Action, error) {
	a := &exists{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	if strings.Contains(a.Include, "${") || strings.Contains(a.Include, "{{") {
		return a, nil // checked once it is resolved
	}
	_, err := kindOf(a.Include)
	return a, err
}

func (a *exists) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	kind, err := kindOf(a.Include)
	if err != nil {
		return action.Result{}, err
	}
	found, err := matchKind(paths.Resolve(ctx, a.Path), kind)
	if err != nil {
		return action.Result{}, err
	}
	if len(found) == 0 {
		return action.Result{Output: []any{}}, fmt.Errorf("no %s matches %q", kindName(kind), a.Path)
	}
	ec.Logf(slog.LevelDebug, "found %d %s for %s", len(found), kindNames(kind, len(found)), a.Path)
	return action.Result{Output: list(found)}, nil
}

// matchKind matches a path and keeps only files, folders or both (kind file, folder or
// any).
func matchKind(path, kind string) ([]string, error) {
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
