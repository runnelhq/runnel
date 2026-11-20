package file

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"

	"github.com/runnelhq/runnel/internal/action"
)

// file.list lists the files (and optionally folders) of a folder. Output: a list
// sorted by path, each entry with path, name, size (bytes), modified (date) and
// folder (true/false), e.g. {{ len(result.output) }} or {{ result.output[0].path }}.
var listDefinition = action.Definition{
	Type:        "file.list",
	Category:    action.CategoryFiles,
	Label:       "List Files",
	Description: "Lists the files of a folder; output: path, name, size, modified and folder of each entry",
	Config:      listAction{},
	New:         newList,
}

type listAction struct {
	Folder    string `json:"folder" doc:"Folder to list" schema:"required,example=C:\\in"`
	Pattern   string `json:"pattern" doc:"Names to include, with wildcards, e.g. *.csv" schema:"default=*"`
	Recursive bool   `json:"recursive" doc:"Also list the subfolders" schema:"default=false"`
	Include   string `json:"include" doc:"What to list" schema:"enum=files|folders|all,default=files"`
}

func newList(with map[string]any) (action.Action, error) {
	a := &listAction{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *listAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	if a.Folder == "" {
		return action.Result{}, errors.New("no folder given")
	}
	pattern := a.Pattern
	if pattern == "" {
		pattern = "*"
	}
	if _, err := filepath.Match(pattern, ""); err != nil {
		return action.Result{}, fmt.Errorf("invalid wildcard pattern %q", pattern)
	}
	include := a.Include
	switch include {
	case "":
		include = "files"
	case "files", "folders", "all":
	default:
		return action.Result{}, fmt.Errorf("invalid include %q (expected files, folders or all)", include)
	}
	if !isFolder(a.Folder) {
		return action.Result{}, fmt.Errorf("folder %q does not exist", a.Folder)
	}

	entries := []any{}
	err := filepath.WalkDir(a.Folder, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("cannot read %q: %w", p, plain(err))
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p == a.Folder {
			return nil
		}
		wanted := include == "all" || include == "folders" && d.IsDir() || include == "files" && !d.IsDir()
		if wanted && nameMatches(pattern, d.Name()) {
			info, err := d.Info()
			if err != nil {
				return fmt.Errorf("cannot read %q: %w", p, plain(err))
			}
			entries = append(entries, fileInfo(p, info))
		}
		if d.IsDir() && !a.Recursive {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return action.Result{}, err
	}
	word := "entries"
	if len(entries) == 1 {
		word = "entry"
	}
	ec.Logf(slog.LevelInfo, "found %d %s in %s", len(entries), word, a.Folder)
	return action.Result{Output: entries}, nil
}
