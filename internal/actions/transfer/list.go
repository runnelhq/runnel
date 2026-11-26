package transfer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/action"
)

// transfer.list lists the files (and optionally folders) of a folder on the server of a
// connection, like file.list. Output: a list sorted by path, each entry with path, name,
// size (bytes), modified (date) and folder (true/false); at most 10000 entries.
var listDefinition = action.Definition{
	Type:        "transfer.list",
	Category:    action.CategoryNetwork,
	Label:       "List Files on Server",
	Description: "Lists the files of a folder on a server (SFTP); output: path, name, size, modified and folder of each entry",
	Example:     "connection: partner-sftp\nfolder: /outgoing\npattern: \"*.csv\"\n",
	Config:      listAction{},
	New:         newList,
}

type listAction struct {
	Connection string `json:"connection" doc:"Connection of the server (a file in connections/)" schema:"required,connection=sftp,example=partner-sftp"`
	Folder     string `json:"folder" doc:"Folder on the server, e.g. /outgoing (relative: to the folder after the sign-in, . is that folder)" schema:"required,example=/outgoing"`
	Pattern    string `json:"pattern" doc:"Names to include, with wildcards that match case-sensitively, e.g. *.csv" schema:"default=*"`
	Recursive  bool   `json:"recursive" doc:"Also list the subfolders" schema:"default=false"`
	Include    string `json:"include" doc:"What to list" schema:"enum=files|folders|all,default=files"`
	Timeout    string `json:"timeout" doc:"Longest time for the listing, e.g. 1m; the timeout of the action limits all together" schema:"format=runnel-duration,default=10m"`
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
	timeout, err := timeoutOf(a.Timeout)
	if err != nil {
		return action.Result{}, err
	}
	if strings.TrimSpace(a.Folder) == "" {
		return action.Result{}, errors.New("no folder given")
	}
	pattern := a.Pattern
	if pattern == "" {
		pattern = "*"
	}
	if _, err := path.Match(pattern, ""); err != nil {
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
	s, err := open(ctx, ec, a.Connection)
	if err != nil {
		return action.Result{}, err
	}
	defer s.close()
	entries := []any{}
	err = s.within(ctx, timeout, "listing "+a.Folder, func() error {
		info, err := s.stat(a.Folder)
		if err != nil {
			return err
		}
		if info == nil || !info.IsDir() {
			return fmt.Errorf("folder %q does not exist on the server", a.Folder)
		}
		return s.walk(ctx, a.Folder, a.Recursive, func(p string, info fs.FileInfo) error {
			wanted := include == "all" || include == "folders" && info.IsDir() || include == "files" && !info.IsDir()
			if ok, _ := path.Match(pattern, info.Name()); !wanted || !ok {
				return nil
			}
			if len(entries) == maxEntries {
				return fmt.Errorf("%q has more than %d entries; narrow the list with pattern or without recursive", a.Folder, maxEntries)
			}
			entries = append(entries, map[string]any{
				"path":     p,
				"name":     info.Name(),
				"size":     info.Size(),
				"modified": info.ModTime().Truncate(time.Second),
				"folder":   info.IsDir(),
			})
			return nil
		})
	})
	if err != nil {
		return action.Result{}, err
	}
	word := "entries"
	if len(entries) == 1 {
		word = "entry"
	}
	ec.Logf(slog.LevelInfo, "found %d %s in %s on %s", len(entries), word, a.Folder, s.addr)
	return action.Result{Output: entries}, nil
}
