package file

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/paths"
)

// file.list lists the files (and optionally folders) of a folder. Output: a list
// sorted by path, each entry with path, name, size (bytes), modified (date) and
// folder (true/false), e.g. {{ len(result.output) }} or {{ result.output[0].path }}.
var listDefinition = action.Definition{
	Type:        "file.list",
	Category:    action.CategoryFiles,
	Label:       "List Files",
	Description: "Lists the files of a folder; output: path, name, size, modified and folder of each entry",
	Example:     "folder: inbox\npattern: \"*.csv\"\n",
	Config:      listAction{},
	New:         newList,
	Reference: action.Reference{
		Outputs: []action.OutputField{
			{Name: "output[].path", Type: "string", Description: "Path of the entry; the list is sorted by path"},
			{Name: "output[].name", Type: "string", Description: "Name of the file or folder"},
			{Name: "output[].size", Type: "integer", Description: "Size in bytes"},
			{Name: "output[].modified", Type: "datetime", Description: "Time of the last change, to the second"},
			{Name: "output[].folder", Type: "boolean", Description: "true for a folder"},
		},
		Errors: "Fails when the folder does not exist or cannot be read; an empty folder gives an empty list.",
		Limits: []string{"All entries are held in memory as the output; for very many files use the transform file.list."},
	},
}

type listAction struct {
	Folder    string `json:"folder" doc:"Folder to list, relative to the project folder or absolute" schema:"required,example=C:\\in"`
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
	entries := []any{}
	err := Walk(ctx, paths.Resolve(ctx, a.Folder), a.Pattern, a.Include, a.Recursive, func(p string, info fs.FileInfo) error {
		entries = append(entries, fileInfo(p, info))
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrNoFolder) {
			return action.Result{}, fmt.Errorf("folder %q does not exist", a.Folder)
		}
		return action.Result{}, err
	}
	word := "entries"
	if len(entries) == 1 {
		word = "entry"
	}
	ec.Logf(slog.LevelInfo, "found %d %s in %s", len(entries), word, a.Folder)
	return action.Result{Output: entries}, nil
}

// ErrNoFolder is the error of Walk for a folder that does not exist.
var ErrNoFolder = errors.New("no such folder")

// Walk calls fn for the entries of a folder in the order of their paths, as file.list
// lists them (also for the transform file.list): names matching pattern (empty: all),
// include files, folders or all (empty: files), with the subfolders if recursive. A
// missing folder gives an error that errors.Is matches with ErrNoFolder.
func Walk(ctx context.Context, folder, pattern, include string, recursive bool, fn func(path string, info fs.FileInfo) error) error {
	if pattern == "" {
		pattern = "*"
	}
	if _, err := filepath.Match(pattern, ""); err != nil {
		return fmt.Errorf("invalid wildcard pattern %q", pattern)
	}
	switch include {
	case "":
		include = "files"
	case "files", "folders", "all":
	default:
		return fmt.Errorf("invalid include %q (expected files, folders or all)", include)
	}
	if !isFolder(folder) {
		return ErrNoFolder
	}
	return filepath.WalkDir(folder, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("cannot read %q: %w", p, plain(err))
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p == folder {
			return nil
		}
		wanted := include == "all" || include == "folders" && d.IsDir() || include == "files" && !d.IsDir()
		if wanted && nameMatches(pattern, d.Name()) {
			info, err := d.Info()
			if err != nil {
				return fmt.Errorf("cannot read %q: %w", p, plain(err))
			}
			if err := fn(p, info); err != nil {
				return err
			}
		}
		if d.IsDir() && !recursive {
			return filepath.SkipDir
		}
		return nil
	})
}
