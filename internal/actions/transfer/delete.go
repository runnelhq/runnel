package transfer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"

	"github.com/runnelhq/runnel/internal/action"
)

// transfer.delete deletes files on the server of a connection and, with recursive, folders
// with their content, like file.delete. Links are deleted themselves, never what they point
// to. Nothing to delete is fine unless ifMissing: fail is set. Output: the deleted paths.
var deleteDefinition = action.Definition{
	Type:        "transfer.delete",
	Category:    action.CategoryNetwork,
	Label:       "Delete Files on Server",
	Description: "Deletes files or folders on a server (SFTP); output: the deleted paths",
	Example:     "connection: partner-sftp\npath: /outgoing/*.csv\n",
	Config:      deleteAction{},
	New:         newDelete,
}

type deleteAction struct {
	Connection string `json:"connection" doc:"Connection of the server (a file in connections/)" schema:"required,connection=sftp,example=partner-sftp"`
	Path       string `json:"path" doc:"File or folder on the server, e.g. /outgoing/*.csv (relative: to the folder after the sign-in); wildcards are allowed in the name and match case-sensitively" schema:"required,example=/outgoing/*.csv"`
	Recursive  bool   `json:"recursive" doc:"Also delete folders with everything in them" schema:"default=false"`
	IfMissing  string `json:"ifMissing" doc:"If nothing matches the path: skip (nothing is deleted) or fail" schema:"enum=skip|fail,default=skip"`
	Timeout    string `json:"timeout" doc:"Longest time for each matching file or folder with its content, e.g. 5m; the timeout of the action limits all together" schema:"format=runnel-duration,default=10m"`
}

func newDelete(with map[string]any) (action.Action, error) {
	a := &deleteAction{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	return a, checkChoice("ifMissing", a.IfMissing, "skip", "fail")
}

func (a *deleteAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	timeout, err := timeoutOf(a.Timeout)
	if err != nil {
		return action.Result{}, err
	}
	s, err := open(ctx, ec, a.Connection)
	if err != nil {
		return action.Result{}, err
	}
	defer s.close()
	var found []entry
	err = s.within(ctx, timeout, "finding the files on the server", func() (err error) {
		found, err = s.find(a.Path, false, maxEntries)
		return err
	})
	if err != nil {
		return action.Result{Output: []any{}}, err
	}
	if len(found) == 0 {
		if a.IfMissing == "fail" {
			return action.Result{Output: []any{}}, fmt.Errorf("nothing on the server matches %q", a.Path)
		}
		ec.Logf(slog.LevelDebug, "nothing to delete for %s", a.Path)
		return action.Result{Output: []any{}}, nil
	}
	// Folders are checked first, so that nothing is deleted if one stops the action.
	for _, e := range found {
		if e.info.IsDir() && !a.Recursive {
			return action.Result{Output: []any{}}, fmt.Errorf("cannot delete %q: it is a folder on the server (set recursive: true to delete it with its content)", e.path)
		}
	}
	var deleted []string
	for _, e := range found {
		err := s.within(ctx, timeout, "deleting "+e.path, func() error { return s.removeAll(ctx, e.path, e.info) })
		if err != nil {
			return action.Result{Output: list(deleted)}, err
		}
		deleted = append(deleted, e.path)
	}
	ec.Logf(slog.LevelInfo, "deleted %d %s on %s", len(deleted), plural(len(deleted), "item"), s.addr)
	return action.Result{Output: list(deleted)}, nil
}

// removeAll deletes a file or link, or a folder with everything in it, without following
// links. Something deleted in the meantime is fine.
func (s *session) removeAll(ctx context.Context, p string, info fs.FileInfo) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var err error
	if !info.IsDir() {
		err = s.sftp.Remove(p)
	} else {
		entries, rerr := s.readDir(p)
		if rerr != nil {
			return rerr
		}
		for _, e := range entries {
			if !remoteName(e.Name()) {
				return fmt.Errorf("cannot delete %q: the server sent the unsafe name %q", p, e.Name())
			}
			if err := s.removeAll(ctx, path.Join(p, e.Name()), e); err != nil {
				return err
			}
		}
		err = s.sftp.RemoveDirectory(p)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("cannot delete %q on the server: %w", p, plain(err))
	}
	return nil
}
