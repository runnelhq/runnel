package transfer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
)

// transfer.mkdir creates a folder on the server of a connection with all missing parent
// folders, like file.mkdir; an existing folder is fine. Output: the path of the folder.
var mkdirDefinition = action.Definition{
	Type:        "transfer.mkdir",
	Category:    action.CategoryNetwork,
	Label:       "Create Folder on Server",
	Description: "Creates a folder with its parent folders on a server (SFTP, FTPS or FTP); output: the path",
	Example:     "connection: partner-sftp\npath: \"/outgoing/archive/{{ formatDate(today(), 'yyyy-MM-dd') }}\"\n",
	Config:      mkdirAction{},
	New:         newMkdir,
}

type mkdirAction struct {
	Connection string `json:"connection" doc:"Connection of the server (a file in connections/)" schema:"required,connection=sftp|ftps|ftp,example=partner-sftp"`
	Path       string `json:"path" doc:"Folder to create on the server, e.g. /outgoing/archive (relative: to the folder after the sign-in)" schema:"required,example=/outgoing/archive"`
	Timeout    string `json:"timeout" doc:"Longest time to create the folder, e.g. 1m; the timeout of the action limits all together" schema:"format=runnel-duration,default=10m"`
}

func newMkdir(with map[string]any) (action.Action, error) {
	a := &mkdirAction{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *mkdirAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	timeout, err := timeoutOf(a.Timeout)
	if err != nil {
		return action.Result{}, err
	}
	if strings.TrimSpace(a.Path) == "" {
		return action.Result{}, errors.New("no path given")
	}
	folder := path.Clean(a.Path)
	s, err := open(ctx, ec, a.Connection)
	if err != nil {
		return action.Result{}, err
	}
	defer s.close()
	created := false
	err = s.within(ctx, timeout, "creating "+folder, func() error {
		info, err := s.stat(folder)
		switch {
		case err != nil:
			return err
		case info != nil && !info.IsDir():
			return fmt.Errorf("cannot create folder %q: a file with this name exists on the server", a.Path)
		case info != nil:
			return nil
		}
		if err := s.fs.MkdirAll(folder); err != nil {
			return fmt.Errorf("cannot create folder %q on the server: %w", a.Path, plain(err))
		}
		created = true
		return nil
	})
	if err != nil {
		return action.Result{}, err
	}
	if created {
		ec.Logf(slog.LevelDebug, "created folder %s on %s", folder, s.addr)
	} else {
		ec.Logf(slog.LevelDebug, "folder %s exists already on %s", folder, s.addr)
	}
	return action.Result{Output: folder}, nil
}
