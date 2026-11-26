package transfer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions/file"
	"github.com/runnelhq/runnel/internal/paths"
)

// transfer.upload copies local files or folders to the server of a connection, like
// file.copy: with wildcards several files go into the target folder, a single file may get
// a new name. Existing files are replaced or skipped with ifExists; by default the action
// fails before anything is uploaded. Each file is written to a temporary name on the server
// and renamed when complete. Output: the paths of the uploaded files on the server.
var uploadDefinition = action.Definition{
	Type:        "transfer.upload",
	Category:    action.CategoryNetwork,
	Label:       "Upload Files",
	Description: "Uploads files or folders to a server (SFTP); output: the uploaded files",
	Example:     "connection: partner-sftp\nsource: outbox/*.csv\ntarget: /incoming/\n",
	Config:      uploadAction{},
	New:         newUpload,
}

type uploadAction struct {
	Connection string `json:"connection" doc:"Connection of the server (a file in connections/)" schema:"required,connection=sftp,example=partner-sftp"`
	Source     string `json:"source" doc:"Local file or folder, relative to the project folder or absolute; wildcards are allowed in the name, e.g. *.csv" schema:"required,example=outbox/*.csv"`
	Target     string `json:"target" doc:"Folder on the server, e.g. /incoming/ (relative: to the folder after the sign-in); for a single file also a new file name" schema:"required,example=/incoming/"`
	IfExists   string `json:"ifExists" doc:"If a file exists on the server: replace it, skip it (it stays and is not in the output), or fail before anything is uploaded" schema:"enum=replace|skip|fail,default=fail"`
	Timeout    string `json:"timeout" doc:"Longest time for one file, e.g. 30m; the timeout of the action limits all together" schema:"format=runnel-duration,default=10m"`
}

func newUpload(with map[string]any) (action.Action, error) {
	a := &uploadAction{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	return a, checkIfExists(a.IfExists)
}

func (a *uploadAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	timeout, err := timeoutOf(a.Timeout)
	if err != nil {
		return action.Result{}, err
	}
	sources, err := file.Match(paths.Resolve(ctx, a.Source))
	if err != nil {
		return action.Result{}, err
	}
	if len(sources) == 0 {
		return action.Result{}, fmt.Errorf("no file or folder matches %q", a.Source)
	}
	if strings.TrimSpace(a.Target) == "" {
		return action.Result{}, errors.New("no target given")
	}
	s, err := open(ctx, ec, a.Connection)
	if err != nil {
		return action.Result{}, err
	}
	defer s.close()
	var pairs [][2]string
	err = s.within(ctx, timeout, "checking the target on the server", func() error {
		pairs, err = a.targets(s, sources)
		return err
	})
	if err != nil {
		return action.Result{}, err
	}
	var written []string
	for _, p := range pairs {
		ec.Logf(slog.LevelDebug, "uploading %s to %s", p[0], p[1])
		files, err := a.upload(ctx, s, timeout, p[0], p[1])
		written = append(written, files...)
		if err != nil {
			return action.Result{Output: list(written)}, err
		}
	}
	ec.Logf(slog.LevelInfo, "uploaded %d %s to %s on %s", len(written), plural(len(written), "file"), a.Target, s.addr)
	return action.Result{Output: list(written)}, nil
}

// targets pairs each local source with its path on the server and reports conflicts
// before anything is written (as file.copy).
func (a *uploadAction) targets(s *session, sources []string) ([][2]string, error) {
	target := a.Target
	info, err := s.stat(target)
	if err != nil {
		return nil, err
	}
	intoFolder := localWildcard(a.Source) || strings.HasSuffix(target, "/") || info != nil && info.IsDir() && !isFolder(sources[0])
	pairs := make([][2]string, len(sources))
	for i, src := range sources {
		dst := target
		if intoFolder {
			dst = path.Join(target, filepath.Base(src))
		}
		pairs[i] = [2]string{src, dst}
		existing, err := s.stat(dst)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			continue
		}
		srcIsFolder := isFolder(src)
		switch {
		case existing.IsDir() && !srcIsFolder:
			return nil, fmt.Errorf("cannot upload %q: %q already exists as a folder on the server", src, dst)
		case !existing.IsDir() && srcIsFolder:
			return nil, fmt.Errorf("cannot upload %q: a file %q exists on the server", src, dst)
		case !existing.IsDir() && (a.IfExists == "" || a.IfExists == "fail"):
			return nil, fmt.Errorf("cannot upload %q: %q already exists on the server (set ifExists: replace or skip)", src, dst)
		}
	}
	return pairs, nil
}

// upload uploads a file, or a folder with everything in it; it returns the files written.
func (a *uploadAction) upload(ctx context.Context, s *session, timeout time.Duration, src, dst string) ([]string, error) {
	info, err := os.Stat(src)
	if err != nil {
		return nil, fmt.Errorf("cannot read %q: %w", src, plain(err))
	}
	one := func(local string, info fs.FileInfo, remote string) (bool, error) {
		err := s.within(ctx, timeout, "the upload of "+local, func() error { return s.put(local, info, remote, a.IfExists) })
		if errors.Is(err, errSkipped) {
			return false, nil
		}
		return err == nil, err
	}
	if !info.IsDir() {
		ok, err := one(src, info, dst)
		if !ok {
			return nil, err
		}
		return []string{dst}, nil
	}
	var written []string
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("cannot read %q: %w", p, plain(err))
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		remote := path.Join(dst, filepath.ToSlash(rel))
		if d.IsDir() {
			return s.within(ctx, timeout, "creating "+remote, func() error {
				if err := s.sftp.MkdirAll(remote); err != nil {
					return fmt.Errorf("cannot create folder %q on the server: %w", remote, plain(err))
				}
				return nil
			})
		}
		fi, err := d.Info()
		if err != nil {
			return fmt.Errorf("cannot read %q: %w", p, plain(err))
		}
		ok, err := one(p, fi, remote)
		if ok {
			written = append(written, remote)
		}
		return err
	})
	return written, err
}

// isFolder reports whether a local path is an existing folder.
func isFolder(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
