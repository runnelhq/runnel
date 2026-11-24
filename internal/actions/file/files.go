// Package file contains the file actions of the namespace "file" (roadmap S09):
// file.exists, file.wait, file.copy, file.move, file.delete, file.mkdir and file.list.
//
// Paths may contain wildcards (*, ?, [abc]) in their last element, e.g. C:\in\*.csv;
// on Windows they match regardless of upper and lower case, like in Explorer. Relative
// paths refer to the project folder (ADR 0020, package paths). Each action lives in a file of its
// own (<name>.go) with its tests next to it; Register lists all of them (ADR 0013).
package file

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/paths"
)

// hasWildcard reports whether the last element of a path contains wildcards.
func hasWildcard(path string) bool {
	return strings.ContainsAny(filepath.Base(path), "*?[")
}

// match returns the existing files and folders a path stands for: the path itself, or
// the entries of its folder whose names match the wildcards of its last element. The
// result is sorted; a missing path or folder gives no matches and no error.
func match(path string) ([]string, error) {
	if path == "" {
		return nil, errors.New("no path given")
	}
	if !hasWildcard(path) {
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, nil
			}
			return nil, fmt.Errorf("cannot read %q: %w", path, plain(err))
		}
		return []string{path}, nil
	}
	dir, pattern := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	if _, err := filepath.Match(pattern, ""); err != nil {
		return nil, fmt.Errorf("invalid wildcard pattern %q", pattern)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read folder %q: %w", dir, plain(err))
	}
	var out []string
	for _, e := range entries {
		if nameMatches(pattern, e.Name()) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out, nil
}

// Match is match for other namespaces (archive.create): the files and folders a path
// with or without wildcards stands for, sorted; nothing found is no error.
func Match(path string) ([]string, error) { return match(path) }

// NameMatches is nameMatches for other namespaces (archive.extract).
func NameMatches(pattern, name string) bool { return nameMatches(pattern, name) }

// nameMatches matches a file name against wildcards; on Windows regardless of case.
func nameMatches(pattern, name string) bool {
	if runtime.GOOS == "windows" {
		pattern, name = strings.ToLower(pattern), strings.ToLower(name)
	}
	ok, _ := filepath.Match(pattern, name)
	return ok
}

// plain returns the reason of a file system error without the operation and path,
// which the messages of the actions name themselves.
func plain(err error) error {
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return pe.Err
	}
	if le, ok := errors.AsType[*os.LinkError](err); ok {
		return le.Err
	}
	return err
}

// list converts paths into a list for result.output.
func list(paths []string) []any {
	out := make([]any, len(paths))
	for i, p := range paths {
		out[i] = p
	}
	return out
}

// isFolder reports whether a path is an existing folder.
func isFolder(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// endsWithSeparator reports whether a path ends with \ or /, i.e. names a folder.
func endsWithSeparator(path string) bool {
	return strings.HasSuffix(path, "/") || strings.HasSuffix(path, string(filepath.Separator))
}

// transfer copies or moves files and folders (file.copy, file.move).
type transfer struct {
	Source   string `json:"source" doc:"File or folder, relative to the project folder or absolute; wildcards are allowed in the name, e.g. *.csv" schema:"required,example=C:\\in\\*.csv"`
	Target   string `json:"target" doc:"Target folder, relative to the project folder or absolute; for a single file also a new file name" schema:"required,example=D:\\archive"`
	IfExists string `json:"ifExists" doc:"If a file exists in the target: replace it, skip it (it stays and is not in the output), or fail before anything is copied or moved" schema:"enum=replace|skip|fail,default=fail"`
}

// errSkipped marks a file that exists in the target and is skipped (ifExists: skip).
var errSkipped = errors.New("skipped")

// renamedTransfer names the fields of file.copy and file.move renamed by ADR 0025.
var renamedTransfer = map[string]string{"overwrite": "renamed to ifExists: overwrite: true is now ifExists: replace"}

// checkIfExists checks the value of ifExists of file.copy and file.move.
func (t *transfer) checkIfExists() error {
	switch t.IfExists {
	case "", "replace", "skip", "fail":
		return nil
	}
	if strings.Contains(t.IfExists, "${") {
		return nil
	}
	return fmt.Errorf("invalid ifExists %q (expected replace, skip or fail)", t.IfExists)
}

// targets pairs each source with its destination. Several sources (wildcards) and paths
// ending with \ go into the target folder; a single file or folder becomes the target.
func (t *transfer) targets(ctx context.Context) ([][2]string, error) {
	if t.Target == "" {
		return nil, errors.New("no target given")
	}
	target := paths.Resolve(ctx, t.Target)
	sources, err := match(paths.Resolve(ctx, t.Source))
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("no file or folder matches %q", t.Source)
	}
	intoFolder := hasWildcard(t.Source) || endsWithSeparator(t.Target) || isFolder(target) && !isFolder(sources[0])
	pairs := make([][2]string, len(sources))
	for i, s := range sources {
		dst := target
		if intoFolder {
			dst = filepath.Join(target, filepath.Base(s))
		}
		if sameFile(s, dst) {
			return nil, fmt.Errorf("%q and %q are the same", s, dst)
		}
		pairs[i] = [2]string{s, dst}
	}
	return pairs, nil
}

// check reports existing targets before anything is copied or moved, so that a conflict
// does not leave a half-done transfer behind. verb is "copy" or "move".
func (t *transfer) check(pairs [][2]string, verb string) error {
	for _, p := range pairs {
		info, err := os.Stat(p[1])
		if err != nil {
			continue
		}
		srcIsFolder := isFolder(p[0])
		switch {
		case info.IsDir() && (!srcIsFolder || verb == "move"):
			return fmt.Errorf("cannot %s %q: %q already exists as a folder", verb, p[0], p[1])
		case !info.IsDir() && srcIsFolder:
			return fmt.Errorf("cannot %s %q: a file %q exists", verb, p[0], p[1])
		case !info.IsDir() && (t.IfExists == "" || t.IfExists == "fail"):
			return fmt.Errorf("cannot %s %q: %q already exists (set ifExists: replace or skip)", verb, p[0], p[1])
		}
	}
	return nil
}

func sameFile(a, b string) bool {
	ia, err1 := os.Stat(a)
	ib, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(ia, ib)
}

// copyPath copies a file, or a folder with everything in it; it returns the files written.
func copyPath(ctx context.Context, src, dst, ifExists string) ([]string, error) {
	info, err := os.Stat(src)
	if err != nil {
		return nil, fmt.Errorf("cannot read %q: %w", src, plain(err))
	}
	if !info.IsDir() {
		if err := copyFile(ctx, src, dst, info, ifExists); err != nil {
			if errors.Is(err, errSkipped) {
				return nil, nil
			}
			return nil, err
		}
		return []string{dst}, nil
	}
	if within(dst, src) {
		return nil, fmt.Errorf("cannot copy %q into itself (%q)", src, dst)
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
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("cannot create folder %q: %w", target, plain(err))
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return fmt.Errorf("cannot read %q: %w", p, plain(err))
		}
		if err := copyFile(ctx, p, target, fi, ifExists); err != nil {
			if errors.Is(err, errSkipped) {
				return nil
			}
			return err
		}
		written = append(written, target)
		return nil
	})
	return written, err
}

// within reports whether path lies inside (or is) dir.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// copyFile copies one file via a temporary file, keeps its modification time and
// stops between blocks when the run is cancelled.
func copyFile(ctx context.Context, src, dst string, info fs.FileInfo, ifExists string) (err error) {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cannot copy %q: not a regular file", src)
	}
	if t, err := os.Stat(dst); err == nil {
		switch {
		case t.IsDir():
			return fmt.Errorf("cannot copy %q to %q: a folder with this name exists", src, dst)
		case ifExists == "skip":
			return errSkipped
		case ifExists != "replace":
			return fmt.Errorf("cannot copy %q: %q already exists (set ifExists: replace or skip)", src, dst)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("cannot create folder %q: %w", filepath.Dir(dst), plain(err))
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("cannot open %q: %w", src, plain(err))
	}
	defer func() { _ = in.Close() }()
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*.tmp")
	if err != nil {
		return fmt.Errorf("cannot write to %q: %w", filepath.Dir(dst), plain(err))
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = io.Copy(tmp, ctxReader{ctx, in}); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("cannot copy %q to %q: %w", src, dst, plain(err))
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("cannot write %q: %w", dst, plain(err))
	}
	_ = os.Chtimes(tmp.Name(), info.ModTime(), info.ModTime())
	if err = os.Rename(tmp.Name(), dst); err != nil {
		return fmt.Errorf("cannot write %q: %w", dst, plain(err))
	}
	return nil
}

// ctxReader stops reading when the context is cancelled.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (r ctxReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// fileInfo describes a file or folder for result.output of file.list.
func fileInfo(path string, info fs.FileInfo) map[string]any {
	return map[string]any{
		"path":     path,
		"name":     info.Name(),
		"size":     info.Size(),
		"modified": info.ModTime().Truncate(time.Second),
		"folder":   info.IsDir(),
	}
}

// sizeOr is the size as written, or the default for an empty one.
func sizeOr(text, fallback string) string {
	if strings.TrimSpace(text) == "" {
		return fallback
	}
	return text
}
