package archive

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions/file"
	"github.com/runnelhq/runnel/internal/paths"
)

// archive.extract unpacks a ZIP, TAR or TAR.GZ archive into a folder. Archives often come
// from outside, so entries that would leave the folder stop the action before anything
// is written, links and special files are left out, and the size and number of entries
// are limited. Output: the paths of the unpacked files.
var extractDefinition = action.Definition{
	Type:        "archive.extract",
	Category:    action.CategoryFiles,
	Label:       "Extract Archive",
	Description: "Unpacks a ZIP, TAR or TAR.GZ archive into a folder; output: the unpacked files",
	Example:     "source: inbox/delivery.zip\ntarget: inbox/delivery\npattern: \"*.csv\"\n",
	Config:      extract{},
	New:         newExtract,
}

type extract struct {
	Source   string `json:"source" doc:"Archive to unpack, relative to the project folder or absolute" schema:"required,example=inbox/delivery.zip"`
	Target   string `json:"target" doc:"Folder to unpack into; it is created if missing" schema:"required,example=inbox/delivery"`
	Format   string `json:"format" doc:"Format of the archive; auto: by the ending of source, else by its content" schema:"enum=auto|zip|tar|tar.gz,default=auto"`
	Pattern  string `json:"pattern" doc:"Only files whose name matches, e.g. *.csv; empty: all files" schema:"example=*.csv"`
	IfExists string `json:"ifExists" doc:"What happens to files that exist in the folder: fail before anything is written, replace them, or skip them" schema:"enum=fail|replace|skip,default=fail"`
	MaxSize  string `json:"maxSize" doc:"Largest total size of the unpacked files, e.g. 500MB or 10GB; protects against archives that unpack to far more than they hold" schema:"default=1GB"`
	MaxFiles int    `json:"maxFiles" doc:"Largest number of entries in the archive" schema:"default=10000"`
}

const (
	defaultMaxSize  = 1 << 30
	defaultMaxFiles = 10000
)

func newExtract(with map[string]any) (action.Action, error) {
	a := &extract{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	if err := checkFormat(a.Format); err != nil {
		return nil, err
	}
	switch a.IfExists {
	case "", "fail", "replace", "skip":
	default:
		return nil, fmt.Errorf("invalid ifExists %q (expected fail, replace or skip)", a.IfExists)
	}
	if _, err := path.Match(a.Pattern, ""); err != nil {
		return nil, fmt.Errorf("pattern: invalid wildcard pattern %q", a.Pattern)
	}
	if a.MaxFiles < 0 {
		return nil, errors.New("maxFiles: must not be negative")
	}
	if !strings.Contains(a.MaxSize, "${") {
		if _, err := size("maxSize", a.MaxSize, defaultMaxSize); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// member is an entry of an archive with its name as a path inside the folder ("" if it
// would lead outside), its kind and the size it states.
type member struct {
	name    string
	dir     bool
	regular bool
	size    int64
	modTime time.Time
}

func (a *extract) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	maxSize, err := size("maxSize", a.MaxSize, defaultMaxSize)
	if err != nil {
		return action.Result{}, err
	}
	maxFiles := a.MaxFiles
	if maxFiles == 0 {
		maxFiles = defaultMaxFiles
	}
	target := paths.Resolve(ctx, a.Target)
	f, err := os.Open(paths.Resolve(ctx, a.Source))
	if err != nil {
		return action.Result{}, fmt.Errorf("cannot read %q: %w", a.Source, plain(err))
	}
	defer func() { _ = f.Close() }()
	format, err := a.format(f)
	if err != nil {
		return action.Result{}, fmt.Errorf("%s: %w", a.Source, err)
	}
	tooLarge := fmt.Errorf("%s unpacks to more than %s; set maxSize if this is intended", a.Source, a.maxSizeText())

	// First pass: check every entry before anything is written.
	var count, skipped int
	var stated int64
	err = walk(ctx, f, format, func(m member, _ func() (io.ReadCloser, error)) error {
		if count++; count > maxFiles {
			return fmt.Errorf("the archive has more than %d entries; set maxFiles if this is intended", maxFiles)
		}
		switch {
		case m.name == "":
			return errors.New("an entry would be unpacked outside the folder; nothing is unpacked")
		case !m.dir && !m.regular:
			skipped++
		case m.regular && a.wanted(m):
			if stated += m.size; stated > maxSize || m.size < 0 {
				return tooLarge
			}
			if (a.IfExists == "" || a.IfExists == "fail") && exists(filepath.Join(target, filepath.FromSlash(m.name))) {
				return fmt.Errorf("%q already exists in %s (set ifExists: replace or skip)", m.name, a.Target)
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, tooLarge) || ctx.Err() != nil {
			return action.Result{}, err
		}
		return action.Result{}, fmt.Errorf("%s: %w", a.Source, err)
	}
	if skipped > 0 {
		ec.Logf(slog.LevelWarn, "%d %s not unpacked: links and special files are left out", skipped, plural(skipped, "entry"))
	}

	// Second pass: unpack. The sizes are counted again, since an archive may state less
	// than it holds.
	written := []any{}
	var total int64
	err = walk(ctx, f, format, func(m member, open func() (io.ReadCloser, error)) error {
		dst := filepath.Join(target, filepath.FromSlash(m.name))
		switch {
		case m.dir && a.Pattern == "":
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return fmt.Errorf("cannot create %q: %w", dst, plain(err))
			}
			return nil
		case !m.regular || !a.wanted(m) || a.IfExists == "skip" && exists(dst):
			return nil
		}
		n, err := unpack(ctx, open, dst, maxSize-total)
		total += n
		switch {
		case errors.Is(err, errTooLarge):
			return tooLarge
		case err != nil && ctx.Err() != nil:
			return ctx.Err()
		case err != nil:
			return fmt.Errorf("cannot unpack %q from %s: %w", m.name, a.Source, plain(err))
		}
		if !m.modTime.IsZero() {
			_ = os.Chtimes(dst, m.modTime, m.modTime)
		}
		written = append(written, dst)
		return nil
	})
	if err != nil {
		return action.Result{Output: written}, err
	}
	ec.Logf(slog.LevelInfo, "unpacked %d %s from %s into %s", len(written), plural(len(written), "file"), a.Source, a.Target)
	return action.Result{Output: written}, nil
}

// wanted reports whether a file matches the pattern.
func (a *extract) wanted(m member) bool {
	return a.Pattern == "" || file.NameMatches(a.Pattern, path.Base(m.name))
}

func (a *extract) maxSizeText() string {
	if strings.TrimSpace(a.MaxSize) == "" {
		return "1GB"
	}
	return a.MaxSize
}

// format returns the format of the archive: as set, by the name or by the content.
func (a *extract) format(f *os.File) (string, error) {
	if a.Format != "" && a.Format != formatAuto {
		return a.Format, nil
	}
	if format := byName(a.Source); format != "" {
		return format, nil
	}
	if format := byContent(bufio.NewReader(f)); format != "" {
		return format, nil
	}
	return "", errors.New("not a ZIP, TAR or TAR.GZ archive")
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

var errTooLarge = errors.New("too large")

// unpack writes a file via a temporary file next to it, with at most limit bytes, and
// returns the bytes read.
func unpack(ctx context.Context, open func() (io.ReadCloser, error), dst string, limit int64) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	r, err := open()
	if err != nil {
		return 0, err
	}
	defer func() { _ = r.Close() }()
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*.tmp")
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(tmp, io.LimitReader(ctxReader{ctx.Err, r}, limit+1))
	if err == nil && n > limit {
		err = errTooLarge
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), dst)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return n, err
}

// walk calls fn for each entry of the archive in its order; open gives the content of
// the entry and is valid only during the call.
func walk(ctx context.Context, f *os.File, format string, fn func(m member, open func() (io.ReadCloser, error)) error) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if format == formatZip {
		info, err := f.Stat()
		if err != nil {
			return err
		}
		zr, err := zip.NewReader(f, info.Size())
		if err != nil {
			return fmt.Errorf("not a readable ZIP archive (%w)", err)
		}
		for _, zf := range zr.File {
			if err := ctx.Err(); err != nil {
				return err
			}
			name := zf.Name
			if zf.Flags&0x800 == 0 && !isASCII(name) {
				// Without the UTF-8 flag, names are in the code page of DOS, as Windows
				// Explorer writes them.
				if d, err := charmap.CodePage437.NewDecoder().String(name); err == nil {
					name = d
				}
			}
			mode := zf.Mode()
			m := member{name: clean(name), dir: mode.IsDir(), regular: mode.IsRegular(),
				size: int64(zf.UncompressedSize64), modTime: zf.Modified} //nolint:gosec // a size above MaxInt64 turns negative and is rejected as too large
			if err := fn(m, zf.Open); err != nil {
				return err
			}
		}
		return nil
	}
	var r io.Reader = bufio.NewReader(f)
	if format == formatTarGz {
		gz, err := gzip.NewReader(r)
		if err != nil {
			return fmt.Errorf("not a readable TAR.GZ archive (%w)", err)
		}
		defer func() { _ = gz.Close() }()
		r = gz
	}
	tr := tar.NewReader(ctxReader{ctx.Err, r})
	open := func() (io.ReadCloser, error) { return io.NopCloser(tr), nil }
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("not a readable TAR archive (%w)", err)
		}
		m := member{name: clean(h.Name), dir: h.Typeflag == tar.TypeDir, size: h.Size, modTime: h.ModTime,
			regular: h.Typeflag == tar.TypeReg}
		if err := fn(m, open); err != nil {
			return err
		}
	}
}

// clean returns the name of an entry as a path inside the folder with / as separator;
// empty if it would lead outside: .., absolute paths, drive letters, reserved names.
func clean(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, ":") || !utf8.ValidString(name) ||
		strings.Contains("/"+name+"/", "/../") {
		return ""
	}
	p := path.Clean(name)
	if p == "." || !filepath.IsLocal(filepath.FromSlash(p)) {
		return ""
	}
	return p
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
