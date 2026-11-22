package file

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/runnelhq/runnel/internal/actions/file"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// file.list reads the entries of a folder as records, like the action file.list:
// path, name, extension, size (bytes), modified and folder (true/false), in the order
// of their paths. It starts a pipeline that processes files, e.g. with
// core.run-pipeline per file or to report on a folder.
var listDefinition = transform.Definition{
	Type:        "file.list",
	Category:    transform.CategoryInput,
	Label:       "List Files",
	Description: "Reads the files of a folder as records: path, name, extension, size, modified and folder",
	Example:     "folder: inbox\npattern: \"*.csv\"\nrecursive: true\n",
	Config:      list{},
	New:         newList,
}

type list struct {
	Folder    string `json:"folder" doc:"Folder to list, relative to the project folder or absolute" schema:"required,example=inbox"`
	Pattern   string `json:"pattern" doc:"Names to include, with wildcards, e.g. *.csv" schema:"default=*"`
	Recursive bool   `json:"recursive" doc:"Also list the subfolders" schema:"default=false"`
	Include   string `json:"include" doc:"What to list" schema:"enum=files|folders|all,default=files"`
}

func newList(with map[string]any) (transform.Transform, error) {
	t := &list{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	if _, err := filepath.Match(t.Pattern, ""); err != nil {
		return nil, fmt.Errorf("pattern: invalid wildcard pattern %q", t.Pattern)
	}
	switch t.Include {
	case "", "files", "folders", "all":
	default:
		return nil, fmt.Errorf("invalid include %q (expected files, folders or all)", t.Include)
	}
	return t, nil
}

// schema of the records: the same fields for every entry.
var schema = record.Schema{}.
	With("path", record.String).
	With("name", record.String).
	With("extension", record.String).
	With("size", record.Int).
	With("modified", record.DateTime).
	With("folder", record.Bool)

// Init reports the closed schema of the records.
func (t *list) Init(*transform.InitContext) (record.Schema, error) {
	return schema, nil
}

func (t *list) Process(ctx context.Context, _ <-chan transform.Batch, out *transform.Output) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	folder := paths.Resolve(ctx, t.Folder)
	err := file.Walk(ctx, folder, t.Pattern, t.Include, t.Recursive, func(p string, info fs.FileInfo) error {
		r := record.Make(6)
		r.Set("path", p)
		r.Set("name", info.Name())
		ext := ""
		if !info.IsDir() {
			ext = filepath.Ext(info.Name())
		}
		r.Set("extension", ext)
		r.Set("size", info.Size())
		r.Set("modified", info.ModTime().Truncate(time.Second))
		r.Set("folder", info.IsDir())
		return out.Emit(r)
	})
	if errors.Is(err, file.ErrNoFolder) {
		return fmt.Errorf("folder %q does not exist", t.Folder)
	}
	return err
}
