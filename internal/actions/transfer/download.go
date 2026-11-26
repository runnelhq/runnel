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
	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/paths"
)

// transfer.download copies files or folders from the server of a connection to local
// folders, like file.copy: with wildcards several files go into the target folder, a single
// file may get a new name. Existing files are replaced or skipped with ifExists; by default
// the action fails before anything is downloaded. Each file is written to a temporary file
// and renamed when complete, so no half file remains. Names from the server that are no
// safe local names (e.g. with \ or ..) are an error. Output: the paths of the downloaded files.
var downloadDefinition = action.Definition{
	Type:        "transfer.download",
	Category:    action.CategoryNetwork,
	Label:       "Download Files",
	Description: "Downloads files or folders from a server (SFTP); output: the downloaded files",
	Example:     "connection: partner-sftp\nsource: /outgoing/*.csv\ntarget: inbox/\nifExists: replace\n",
	Config:      downloadAction{},
	New:         newDownload,
}

type downloadAction struct {
	Connection string `json:"connection" doc:"Connection of the server (a file in connections/)" schema:"required,connection=sftp,example=partner-sftp"`
	Source     string `json:"source" doc:"File or folder on the server, e.g. /outgoing/*.csv (relative: to the folder after the sign-in); wildcards are allowed in the name and match case-sensitively" schema:"required,example=/outgoing/*.csv"`
	Target     string `json:"target" doc:"Local folder, relative to the project folder or absolute; for a single file also a new file name" schema:"required,example=inbox/"`
	IfExists   string `json:"ifExists" doc:"If a local file exists: replace it, skip it (it stays and is not in the output), or fail before anything is downloaded" schema:"enum=replace|skip|fail,default=fail"`
	MaxSize    string `json:"maxSize" doc:"Largest file to download, e.g. 500MB; a larger one fails the action; empty: no limit" schema:"example=500MB"`
	Timeout    string `json:"timeout" doc:"Longest time for one file, e.g. 30m; the timeout of the action limits all together" schema:"format=runnel-duration,default=10m"`
}

func newDownload(with map[string]any) (action.Action, error) {
	a := &downloadAction{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	if !strings.Contains(a.MaxSize, "{{") && !strings.Contains(a.MaxSize, "${") {
		if _, err := format.ParseSize("maxSize", a.MaxSize, 0); err != nil {
			return nil, err
		}
	}
	return a, checkIfExists(a.IfExists)
}

func (a *downloadAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	timeout, err := timeoutOf(a.Timeout)
	if err != nil {
		return action.Result{}, err
	}
	maxSize, err := format.ParseSize("maxSize", a.MaxSize, 0)
	if err != nil {
		return action.Result{}, err
	}
	if strings.TrimSpace(a.Target) == "" {
		return action.Result{}, errors.New("no target given")
	}
	s, err := open(ctx, ec, a.Connection)
	if err != nil {
		return action.Result{}, err
	}
	defer s.close()
	var pairs []pair
	err = s.within(ctx, timeout, "finding the files on the server", func() error {
		pairs, err = a.targets(ctx, s)
		return err
	})
	if err != nil {
		return action.Result{}, err
	}
	d := &downloader{a: a, s: s, ctx: ctx, timeout: timeout, max: maxSize}
	for _, p := range pairs {
		ec.Logf(slog.LevelDebug, "downloading %s to %s", p.remote, p.local)
		err := d.download(p.remote, p.info, p.local)
		if err != nil {
			return action.Result{Output: list(d.written)}, err
		}
	}
	ec.Logf(slog.LevelInfo, "downloaded %d %s from %s to %s", len(d.written), plural(len(d.written), "file"), s.addr, a.Target)
	return action.Result{Output: list(d.written)}, nil
}

// pair is an entry on the server and its local path.
type pair struct {
	remote, local string
	info          fs.FileInfo
}

// targets pairs each matching entry on the server with its local path and reports
// conflicts before anything is written (as file.copy).
func (a *downloadAction) targets(ctx context.Context, s *session) ([]pair, error) {
	sources, err := s.match(a.Source)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("nothing on the server matches %q", a.Source)
	}
	target := paths.Resolve(ctx, a.Target)
	infos := make([]fs.FileInfo, len(sources))
	for i, src := range sources {
		if infos[i], err = s.stat(src); err != nil {
			return nil, err
		}
		if infos[i] == nil {
			return nil, fmt.Errorf("%q disappeared from the server", src)
		}
	}
	endsWithSeparator := strings.HasSuffix(a.Target, "/") || strings.HasSuffix(a.Target, string(filepath.Separator))
	intoFolder := hasWildcard(a.Source) || endsWithSeparator || isFolder(target) && !infos[0].IsDir()
	pairs := make([]pair, len(sources))
	for i, src := range sources {
		dst := target
		if intoFolder {
			name := path.Base(src)
			if !localName(name) {
				return nil, fmt.Errorf("cannot download %q: %q is not allowed as a local file name", src, name)
			}
			dst = filepath.Join(target, name)
		}
		pairs[i] = pair{remote: src, local: dst, info: infos[i]}
		existing, err := os.Stat(dst)
		if err != nil {
			continue
		}
		switch {
		case existing.IsDir() && !infos[i].IsDir():
			return nil, fmt.Errorf("cannot download %q: %q already exists as a folder", src, dst)
		case !existing.IsDir() && infos[i].IsDir():
			return nil, fmt.Errorf("cannot download %q: a file %q exists", src, dst)
		case !existing.IsDir() && (a.IfExists == "" || a.IfExists == "fail"):
			return nil, fmt.Errorf("cannot download %q: %q already exists (set ifExists: replace or skip)", src, dst)
		}
	}
	return pairs, nil
}

// downloader downloads the files of one execution.
type downloader struct {
	a       *downloadAction
	s       *session
	ctx     context.Context
	timeout time.Duration
	max     int64
	written []string
}

// download downloads a file, or a folder with everything in it.
func (d *downloader) download(remote string, info fs.FileInfo, local string) error {
	if err := d.ctx.Err(); err != nil {
		return err
	}
	if !info.IsDir() {
		err := d.s.within(d.ctx, d.timeout, "the download of "+remote, func() error {
			return d.s.get(remote, info, local, d.a.IfExists, d.max)
		})
		switch {
		case errors.Is(err, errSkipped):
			return nil
		case errors.Is(err, errTooLarge):
			return fmt.Errorf("cannot download %q: it is larger than maxSize %s", remote, d.a.MaxSize)
		case err != nil:
			return err
		}
		d.written = append(d.written, local)
		return nil
	}
	if err := os.MkdirAll(local, 0o755); err != nil {
		return fmt.Errorf("cannot create folder %q: %w", local, plain(err))
	}
	var entries []fs.FileInfo
	err := d.s.within(d.ctx, d.timeout, "reading "+remote, func() (err error) {
		entries, err = d.s.readDir(remote)
		return err
	})
	if err != nil {
		return err
	}
	for _, e := range entries {
		r := path.Join(remote, e.Name())
		if !localName(e.Name()) {
			return fmt.Errorf("cannot download %q: %q is not allowed as a local file name", r, e.Name())
		}
		if err := d.download(r, e, filepath.Join(local, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
