package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions/file"
	"github.com/runnelhq/runnel/internal/paths"
)

// archive.create packs files and folders into a ZIP, TAR or TAR.GZ archive. A folder is
// packed with its subfolders, under its own name. The archive is written via a temporary
// file and replaces an existing one only once it is complete.
// Output: path, number of files and size of the archive.
var createDefinition = action.Definition{
	Type:        "archive.create",
	Category:    action.CategoryFiles,
	Label:       "Create Archive",
	Description: "Packs files and folders into a ZIP, TAR or TAR.GZ archive; output: path, files and size",
	Example:     "source: out/export\ntarget: archive/export.zip\n",
	Config:      create{},
	New:         newCreate,
}

type create struct {
	Source   string `json:"source" doc:"File or folder to pack, relative to the project folder or absolute; wildcards are allowed in the name, e.g. out/*.csv" schema:"required,example=out/*.csv"`
	Target   string `json:"target" doc:"Archive to write, e.g. archive/export.zip" schema:"required,example=archive/export.zip"`
	Format   string `json:"format" doc:"Format of the archive; auto: by the ending of target (.zip, .tar, .tar.gz or .tgz)" schema:"enum=auto|zip|tar|tar.gz,default=auto"`
	IfExists string `json:"ifExists" doc:"If the archive exists: replace it once the new one is complete, or fail" schema:"enum=replace|fail,default=replace"`
}

func newCreate(with map[string]any) (action.Action, error) {
	a := &create{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	if err := checkFormat(a.Format); err != nil {
		return nil, err
	}
	switch a.IfExists {
	case "", "replace", "fail":
	default:
		return nil, fmt.Errorf("invalid ifExists %q (expected replace or fail)", a.IfExists)
	}
	return a, nil
}

// entry is a file to pack: where it is and its name in the archive.
type entry struct {
	path, name string
	info       fs.FileInfo
}

func (a *create) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	format := a.Format
	if format == "" || format == formatAuto {
		if format = byName(a.Target); format == "" {
			return action.Result{}, fmt.Errorf("target: %q has no known ending (.zip, .tar, .tar.gz); set format", a.Target)
		}
	}
	target := paths.Resolve(ctx, a.Target)
	if info, err := os.Stat(target); err == nil {
		if info.IsDir() {
			return action.Result{}, fmt.Errorf("cannot write %q: it is a folder", a.Target)
		}
		if a.IfExists == "fail" {
			return action.Result{}, fmt.Errorf("%q already exists (ifExists: fail)", a.Target)
		}
	}
	sources, err := file.Match(paths.Resolve(ctx, a.Source))
	if err != nil {
		return action.Result{}, fmt.Errorf("source: %w", err)
	}
	if len(sources) == 0 {
		return action.Result{}, fmt.Errorf("source: nothing found at %q", a.Source)
	}
	entries, skipped, err := collect(sources, target)
	if err != nil {
		return action.Result{}, err
	}
	if skipped > 0 {
		ec.Logf(slog.LevelWarn, "%d %s not packed: only files and folders, no links or devices", skipped, plural(skipped, "entry"))
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return action.Result{}, fmt.Errorf("cannot write %q: %w", a.Target, plain(err))
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return action.Result{}, fmt.Errorf("cannot write %q: %w", a.Target, plain(err))
	}
	err = write(ctx, tmp, format, entries)
	if cerr := tmp.Close(); err == nil && cerr != nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), target)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		if ctx.Err() != nil {
			return action.Result{}, ctx.Err()
		}
		return action.Result{}, fmt.Errorf("cannot write %q: %w", a.Target, plain(err))
	}
	info, err := os.Stat(target)
	if err != nil {
		return action.Result{}, fmt.Errorf("cannot read %q: %w", a.Target, plain(err))
	}
	ec.Logf(slog.LevelInfo, "packed %d %s into %s (%d bytes)", len(entries), plural(len(entries), "file"), a.Target, info.Size())
	return action.Result{Output: map[string]any{"path": target, "files": int64(len(entries)), "size": info.Size()}}, nil
}

// collect lists the files to pack: a file under its name, a folder with its content
// under its own name. Links and other special files are counted, not packed; the
// archive itself is left out when it lies inside a folder that is packed.
func collect(sources []string, target string) (entries []entry, skipped int, err error) {
	for _, src := range sources {
		info, err := os.Lstat(src)
		if err != nil {
			return nil, 0, fmt.Errorf("cannot read %q: %w", src, plain(err))
		}
		if !info.IsDir() {
			if info.Mode().IsRegular() && !sameFile(src, target) {
				entries = append(entries, entry{src, filepath.Base(src), info})
			} else if !info.Mode().IsRegular() {
				skipped++
			}
			continue
		}
		base := filepath.Dir(src)
		err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(base, p)
			switch {
			case d.IsDir():
				entries = append(entries, entry{p, filepath.ToSlash(rel) + "/", info})
			case info.Mode().IsRegular() && !sameFile(p, target):
				entries = append(entries, entry{p, filepath.ToSlash(rel), info})
			case !info.Mode().IsRegular():
				skipped++
			}
			return nil
		})
		if err != nil {
			return nil, 0, fmt.Errorf("cannot read %q: %w", src, plain(err))
		}
	}
	// Folders with something below them come with their entries; empty ones are packed
	// as entries of their own.
	parents := map[string]bool{}
	for _, e := range entries {
		n := strings.TrimSuffix(e.name, "/")
		if i := strings.LastIndex(n, "/"); i >= 0 {
			parents[n[:i+1]] = true
		}
	}
	files := entries[:0]
	for _, e := range entries {
		if !e.info.IsDir() || !parents[e.name] {
			files = append(files, e)
		}
	}
	return files, skipped, nil
}

func sameFile(a, b string) bool {
	x, err1 := filepath.Abs(a)
	y, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && x == y
}

// write packs the entries in the format into w.
func write(ctx context.Context, w io.Writer, format string, entries []entry) error {
	done := ctx.Err
	switch format {
	case formatZip:
		zw := zip.NewWriter(w)
		for _, e := range entries {
			h, err := zip.FileInfoHeader(e.info)
			if err != nil {
				return err
			}
			h.Name = e.name
			if !e.info.IsDir() {
				h.Method = zip.Deflate
			}
			fw, err := zw.CreateHeader(h)
			if err != nil {
				return err
			}
			if err := copyFile(fw, e, done); err != nil {
				return err
			}
		}
		return zw.Close()
	case formatTar, formatTarGz:
		out := w
		var gz *gzip.Writer
		if format == formatTarGz {
			gz = gzip.NewWriter(w)
			out = gz
		}
		tw := tar.NewWriter(out)
		for _, e := range entries {
			h, err := tar.FileInfoHeader(e.info, "")
			if err != nil {
				return err
			}
			h.Name = e.name
			h.Uname, h.Gname = "", ""
			if err := tw.WriteHeader(h); err != nil {
				return err
			}
			if err := copyFile(tw, e, done); err != nil {
				return err
			}
		}
		if err := tw.Close(); err != nil {
			return err
		}
		if gz != nil {
			return gz.Close()
		}
		return nil
	}
	return fmt.Errorf("invalid format %q", format)
}

// copyFile copies the content of a file entry; folders have none.
func copyFile(w io.Writer, e entry, done func() error) error {
	if e.info.IsDir() {
		return done()
	}
	f, err := os.Open(e.path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(w, ctxReader{done, f})
	return err
}
