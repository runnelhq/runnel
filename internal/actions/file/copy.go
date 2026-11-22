package file

import (
	"context"
	"log/slog"

	"github.com/runnelhq/runnel/internal/action"
)

// file.copy copies files or folders. With wildcards, several files go into the target
// folder; a single file may also get a new name. Existing files are only replaced with
// overwrite. Files are written via a temporary file and keep their modification time.
// Output: the paths of the copied files.
var copyDefinition = action.Definition{
	Type:        "file.copy",
	Category:    action.CategoryFiles,
	Label:       "Copy Files",
	Description: "Copies files or folders; output: the copied files",
	Example:     "source: inbox/*.csv\ntarget: archive/\noverwrite: true\n",
	Config:      transfer{},
	New:         newCopy,
}

type copyAction struct{ transfer }

func newCopy(with map[string]any) (action.Action, error) {
	a := &copyAction{}
	if err := action.DecodeConfig(with, &a.transfer); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *copyAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	pairs, err := a.targets(ctx)
	if err == nil {
		err = a.check(pairs, "copy")
	}
	if err != nil {
		return action.Result{}, err
	}
	var written []string
	for _, p := range pairs {
		ec.Logf(slog.LevelDebug, "copying %s to %s", p[0], p[1])
		files, err := copyPath(ctx, p[0], p[1], a.Overwrite)
		written = append(written, files...)
		if err != nil {
			return action.Result{Output: list(written)}, err
		}
	}
	ec.Logf(slog.LevelInfo, "copied %d %s to %s", len(written), plural(len(written), "file"), a.Target)
	return action.Result{Output: list(written)}, nil
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
