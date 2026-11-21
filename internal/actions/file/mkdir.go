package file

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/paths"
)

// file.mkdir creates a folder with all missing parent folders; an existing folder is
// fine. Output: the path of the folder.
var mkdirDefinition = action.Definition{
	Type:        "file.mkdir",
	Category:    action.CategoryFiles,
	Label:       "Create Folder",
	Description: "Creates a folder with its parent folders; output: the path",
	Config:      mkdir{},
	New:         newMkdir,
}

type mkdir struct {
	Path string `json:"path" doc:"Folder to create, relative to the project folder or absolute" schema:"required,example=D:\\archive\\2026"`
}

func newMkdir(with map[string]any) (action.Action, error) {
	a := &mkdir{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *mkdir) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	if a.Path == "" {
		return action.Result{}, errors.New("no path given")
	}
	// The output is the folder with the project folder in front of a relative path, so
	// that later actions find it wherever they look (ADR 0020).
	folder := paths.Resolve(ctx, a.Path)
	if info, err := os.Stat(folder); err == nil {
		if !info.IsDir() {
			return action.Result{}, fmt.Errorf("cannot create folder %q: a file with this name exists", a.Path)
		}
		ec.Logf(slog.LevelDebug, "folder %s exists already", a.Path)
		return action.Result{Output: folder}, nil
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return action.Result{}, fmt.Errorf("cannot create folder %q: %w", a.Path, plain(err))
	}
	ec.Logf(slog.LevelDebug, "created folder %s", a.Path)
	return action.Result{Output: folder}, nil
}
