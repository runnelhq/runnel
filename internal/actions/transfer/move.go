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

// transfer.move moves or renames files and folders on the server of a connection, like
// file.move: with wildcards several entries go into the target folder, a single one may get
// a new name. Existing files are replaced or skipped with ifExists; by default the action
// fails before anything is moved. The server renames (atomically with
// posix-rename@openssh.com where it can); nothing is copied, so the target has to be on the
// same file system of the server. Links are moved themselves. Output: the new paths.
var moveDefinition = action.Definition{
	Type:        "transfer.move",
	Category:    action.CategoryNetwork,
	Label:       "Move Files on Server",
	Description: "Moves or renames files or folders on a server (SFTP, FTPS or FTP); output: the new paths",
	Example:     "connection: partner-sftp\nsource: /outgoing/*.csv\ntarget: /outgoing/archive/\n",
	Config:      moveAction{},
	New:         newMove,
}

type moveAction struct {
	Connection string `json:"connection" doc:"Connection of the server (a file in connections/)" schema:"required,connection=sftp|ftps|ftp,example=partner-sftp"`
	Source     string `json:"source" doc:"File or folder on the server, e.g. /outgoing/*.csv (relative: to the folder after the sign-in); wildcards are allowed in the name and match case-sensitively" schema:"required,example=/outgoing/*.csv"`
	Target     string `json:"target" doc:"Folder on the same server, e.g. /outgoing/archive/; for a single file or folder also a new name" schema:"required,example=/outgoing/archive/"`
	IfExists   string `json:"ifExists" doc:"If a file exists in the target: replace it, skip it (the source stays and is not in the output), or fail before anything is moved" schema:"enum=replace|skip|fail,default=fail"`
	Timeout    string `json:"timeout" doc:"Longest time for each file or folder, e.g. 1m; the timeout of the action limits all together" schema:"format=runnel-duration,default=10m"`
}

func newMove(with map[string]any) (action.Action, error) {
	a := &moveAction{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	return a, checkIfExists(a.IfExists)
}

func (a *moveAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	timeout, err := timeoutOf(a.Timeout)
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
	var pairs []movePair
	err = s.within(ctx, timeout, "checking the files on the server", func() (err error) {
		pairs, err = a.targets(s)
		return err
	})
	if err != nil {
		return action.Result{}, err
	}
	var moved []string
	for _, p := range pairs {
		ec.Logf(slog.LevelDebug, "moving %s to %s", p.from, p.to)
		err := s.within(ctx, timeout, "moving "+p.from, func() error { return s.move(p, a.IfExists) })
		if errors.Is(err, errSkipped) {
			continue
		}
		if err != nil {
			return action.Result{Output: list(moved)}, err
		}
		moved = append(moved, p.to)
	}
	ec.Logf(slog.LevelInfo, "moved %d %s to %s on %s", len(moved), plural(len(moved), "item"), a.Target, s.addr)
	return action.Result{Output: list(moved)}, nil
}

// movePair is an entry on the server and its new path there.
type movePair struct {
	from, to string
	dir      bool
}

// targets pairs each matching entry with its new path and reports conflicts before
// anything is moved (as file.move).
func (a *moveAction) targets(s *session) ([]movePair, error) {
	found, err := s.find(a.Source, false, maxEntries)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("nothing on the server matches %q", a.Source)
	}
	target, err := s.stat(a.Target)
	if err != nil {
		return nil, err
	}
	intoFolder := hasWildcard(a.Source) || strings.HasSuffix(a.Target, "/") || target != nil && target.IsDir() && !found[0].info.IsDir()
	pairs := make([]movePair, len(found))
	for i, e := range found {
		dst := a.Target
		if intoFolder {
			name := path.Base(e.path)
			if !remoteName(name) {
				return nil, fmt.Errorf("cannot move %q: the server sent the unsafe name %q", e.path, name)
			}
			dst = path.Join(a.Target, name)
		}
		if path.Clean(e.path) == path.Clean(dst) {
			return nil, fmt.Errorf("%q and %q are the same", e.path, dst)
		}
		pairs[i] = movePair{from: e.path, to: dst, dir: e.info.IsDir()}
		existing, err := s.stat(dst)
		if err != nil {
			return nil, err
		}
		switch {
		case existing == nil:
		case existing.IsDir():
			return nil, fmt.Errorf("cannot move %q: %q already exists as a folder on the server", e.path, dst)
		case e.info.IsDir():
			return nil, fmt.Errorf("cannot move %q: a file %q exists on the server", e.path, dst)
		case a.IfExists == "" || a.IfExists == "fail":
			return nil, fmt.Errorf("cannot move %q: %q already exists on the server (set ifExists: replace or skip)", e.path, dst)
		}
	}
	return pairs, nil
}

// move renames one entry on the server.
func (s *session) move(p movePair, ifExists string) error {
	existing, err := s.stat(p.to)
	if err != nil {
		return err
	}
	if existing != nil {
		switch {
		case existing.IsDir():
			return fmt.Errorf("cannot move %q: %q already exists as a folder on the server", p.from, p.to)
		case ifExists == "skip":
			return errSkipped // the source stays where it is
		case ifExists != "replace":
			return fmt.Errorf("cannot move %q: %q already exists on the server (set ifExists: replace or skip)", p.from, p.to)
		}
	}
	if src := path.Clean(p.from); p.dir && strings.HasPrefix(path.Clean(p.to), src+"/") {
		return fmt.Errorf("cannot move %q into itself (%q)", p.from, p.to)
	}
	if dir := path.Dir(p.to); dir != "." {
		if err := s.fs.MkdirAll(dir); err != nil {
			return fmt.Errorf("cannot create folder %q on the server: %w", dir, plain(err))
		}
	}
	if err := s.fs.Rename(p.from, p.to, existing != nil); err != nil {
		return fmt.Errorf("cannot move %q to %q on the server: %w", p.from, p.to, plain(err))
	}
	return nil
}
