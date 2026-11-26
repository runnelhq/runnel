package transfer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pkg/sftp"

	"github.com/runnelhq/runnel/internal/model"
)

const (
	// defaultTimeout limits one file (upload, download) or one listing.
	defaultTimeout = 10 * time.Minute
	// maxEntries limits the output of transfer.list: it lies in the result of the run.
	maxEntries = 10000
)

// errSkipped marks a file that exists in the target and is skipped (ifExists: skip).
var errSkipped = errors.New("skipped")

// errTooLarge marks a download larger than maxSize.
var errTooLarge = errors.New("larger than maxSize")

// timeoutOf reads the field timeout of an action.
func timeoutOf(text string) (time.Duration, error) {
	if strings.TrimSpace(text) == "" {
		return defaultTimeout, nil
	}
	d, err := model.Duration(text).Value()
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("timeout: %q is not a duration like 30s or 10m", text)
	}
	return d, nil
}

// checkIfExists checks the value of ifExists, as in file.copy.
func checkIfExists(v string) error {
	switch v {
	case "", "replace", "skip", "fail":
		return nil
	}
	if strings.Contains(v, "${") || strings.Contains(v, "{{") {
		return nil
	}
	return fmt.Errorf("invalid ifExists %q (expected replace, skip or fail)", v)
}

// hasWildcard reports whether the last element of a path on the server contains wildcards.
func hasWildcard(p string) bool {
	return strings.ContainsAny(path.Base(p), "*?[")
}

// localWildcard is hasWildcard for a local path.
func localWildcard(p string) bool {
	return strings.ContainsAny(filepath.Base(p), "*?[")
}

// localName reports whether a name from the server is safe as the name of a local file:
// one element, not "." or "..", no separator, drive or reserved name (Windows).
func localName(name string) bool {
	return name != "" && name != "." && !strings.ContainsAny(name, `/\`) && filepath.IsLocal(name)
}

// plain returns the reason of an error of the server without the prefix of the library.
func plain(err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errors.New("no such file or folder")
	case errors.Is(err, fs.ErrPermission):
		return errors.New("permission denied")
	}
	if se, ok := errors.AsType[*sftp.StatusError](err); ok {
		return errors.New(strings.TrimPrefix(se.Error(), "sftp: "))
	}
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return pe.Err
	}
	return err
}

// stat returns the entry of a path on the server; a missing path is nil without error.
func (s *session) stat(p string) (fs.FileInfo, error) {
	info, err := s.sftp.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read %q on the server: %w", p, plain(err))
	}
	return info, nil
}

// readDir returns the entries of a folder on the server sorted by name.
func (s *session) readDir(dir string) ([]fs.FileInfo, error) {
	entries, err := s.sftp.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read folder %q on the server: %w", dir, plain(err))
	}
	slices.SortFunc(entries, func(a, b fs.FileInfo) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, nil
}

// match returns the entries on the server a path stands for, like file.Match: the path
// itself, or the entries of its folder whose names match the wildcards of its last
// element (case-sensitive, as on most servers). Nothing found is no error.
func (s *session) match(p string) ([]string, error) {
	if strings.TrimSpace(p) == "" {
		return nil, errors.New("no path given")
	}
	if !hasWildcard(p) {
		info, err := s.stat(p)
		if info == nil || err != nil {
			return nil, err
		}
		return []string{p}, nil
	}
	dir, pattern := path.Split(p)
	if dir == "" {
		dir = "."
	}
	if _, err := path.Match(pattern, ""); err != nil {
		return nil, fmt.Errorf("invalid wildcard pattern %q", pattern)
	}
	if info, err := s.stat(dir); info == nil || err != nil {
		return nil, err
	}
	entries, err := s.readDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if ok, _ := path.Match(pattern, e.Name()); ok {
			out = append(out, path.Join(dir, e.Name()))
		}
	}
	return out, nil
}

// walk calls fn for the entries of a folder on the server in the order of their paths,
// with the subfolders if recursive (like filepath.WalkDir). Links are not followed.
func (s *session) walk(ctx context.Context, dir string, recursive bool, fn func(p string, info fs.FileInfo) error) error {
	entries, err := s.readDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		p := path.Join(dir, e.Name())
		if err := fn(p, e); err != nil {
			return err
		}
		if e.IsDir() && recursive {
			if err := s.walk(ctx, p, true, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// tempName returns a hidden temporary name next to a file, e.g. .a.csv.3f9a01bc.tmp.
func tempName(name string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "." + name + "." + hex.EncodeToString(b) + ".tmp"
}

// put uploads one local file via a temporary file on the server, which it renames when
// complete; on an error it removes the temporary file. The file keeps its modification time.
func (s *session) put(local string, info fs.FileInfo, remote, ifExists string) (err error) {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cannot upload %q: not a regular file", local)
	}
	existing, err := s.stat(remote)
	if err != nil {
		return err
	}
	if existing != nil {
		switch {
		case existing.IsDir():
			return fmt.Errorf("cannot upload %q to %q: a folder with this name exists on the server", local, remote)
		case ifExists == "skip":
			return errSkipped
		case ifExists != "replace":
			return fmt.Errorf("cannot upload %q: %q already exists on the server (set ifExists: replace or skip)", local, remote)
		}
	}
	dir := path.Dir(remote)
	if err := s.sftp.MkdirAll(dir); err != nil {
		return fmt.Errorf("cannot create folder %q on the server: %w", dir, plain(err))
	}
	in, err := os.Open(local)
	if err != nil {
		return fmt.Errorf("cannot open %q: %w", local, plain(err))
	}
	defer func() { _ = in.Close() }()
	tmp := path.Join(dir, tempName(path.Base(remote)))
	out, err := s.sftp.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return fmt.Errorf("cannot write to %q on the server: %w", dir, plain(err))
	}
	defer func() {
		if err != nil {
			_ = out.Close()
			_ = s.sftp.Remove(tmp)
		}
	}()
	if _, err = out.ReadFrom(in); err != nil {
		return fmt.Errorf("cannot upload %q to %q: %w", local, remote, plain(err))
	}
	if err = out.Close(); err != nil {
		return fmt.Errorf("cannot write %q on the server: %w", remote, plain(err))
	}
	_ = s.sftp.Chtimes(tmp, info.ModTime(), info.ModTime())
	if err = s.rename(tmp, remote, existing != nil); err != nil {
		return fmt.Errorf("cannot write %q on the server: %w", remote, plain(err))
	}
	return nil
}

// rename gives a complete temporary file its name: atomically where the server can
// (posix-rename@openssh.com), otherwise by removing an existing file first.
func (s *session) rename(from, to string, exists bool) error {
	if s.posixRename {
		return s.sftp.PosixRename(from, to)
	}
	if exists {
		if err := s.sftp.Remove(to); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return s.sftp.Rename(from, to)
}

// get downloads one file via a temporary local file, which it renames when complete; at
// most limit bytes (0: no limit). The file keeps its modification time.
func (s *session) get(remote string, info fs.FileInfo, local, ifExists string, limit int64) (err error) {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cannot download %q: not a regular file", remote)
	}
	if limit > 0 && info.Size() > limit {
		return fmt.Errorf("cannot download %q: %w", remote, errTooLarge)
	}
	if t, err := os.Stat(local); err == nil {
		switch {
		case t.IsDir():
			return fmt.Errorf("cannot download %q to %q: a folder with this name exists", remote, local)
		case ifExists == "skip":
			return errSkipped
		case ifExists != "replace":
			return fmt.Errorf("cannot download %q: %q already exists (set ifExists: replace or skip)", remote, local)
		}
	}
	dir := filepath.Dir(local)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create folder %q: %w", dir, plain(err))
	}
	in, err := s.sftp.Open(remote)
	if err != nil {
		return fmt.Errorf("cannot open %q on the server: %w", remote, plain(err))
	}
	defer func() { _ = in.Close() }()
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(local)+".*.tmp")
	if err != nil {
		return fmt.Errorf("cannot write to %q: %w", dir, plain(err))
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = in.WriteTo(&limitWriter{w: tmp, left: limit}); err != nil {
		if errors.Is(err, errTooLarge) {
			return fmt.Errorf("cannot download %q: %w", remote, errTooLarge)
		}
		return fmt.Errorf("cannot download %q to %q: %w", remote, local, plain(err))
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("cannot write %q: %w", local, plain(err))
	}
	_ = os.Chtimes(tmp.Name(), info.ModTime(), info.ModTime())
	if err = os.Rename(tmp.Name(), local); err != nil {
		return fmt.Errorf("cannot write %q: %w", local, plain(err))
	}
	return nil
}

// limitWriter fails with errTooLarge after left bytes; left 0 is no limit.
type limitWriter struct {
	w    io.Writer
	left int64
	used int64
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.left > 0 && l.used+int64(len(p)) > l.left {
		return 0, errTooLarge
	}
	l.used += int64(len(p))
	return l.w.Write(p)
}

// list converts paths into a list for result.output.
func list(paths []string) []any {
	out := make([]any, len(paths))
	for i, p := range paths {
		out[i] = p
	}
	return out
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
