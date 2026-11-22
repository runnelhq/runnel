package archive

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

// tree writes files (name → content) below dir; names ending with / are folders.
func tree(t testing.TB, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// contents lists the files below dir as name → content, names with /.
func contents(t testing.TB, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || p == dir {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if info.IsDir() {
			out[rel+"/"] = ""
			return nil
		}
		b, _ := os.ReadFile(p)
		out[rel] = string(b)
		return nil
	})
	return out
}

func keys(m map[string]string) string {
	var k []string
	for n := range m {
		k = append(k, n)
	}
	sort.Strings(k)
	return strings.Join(k, ",")
}

func run(t testing.TB, def action.Definition, with map[string]any) (any, []actiontest.Logged, error) {
	t.Helper()
	a, err := def.New(with)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	res, logs, err := actiontest.Execute(context.Background(), a)
	return res.Output, logs, err
}

func TestRoundTrip(t *testing.T) {
	for _, name := range []string{"out.zip", "out.tar", "out.tar.gz", "out.tgz"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			tree(t, dir, map[string]string{"export/a.csv": "A", "export/sub/b.txt": "B", "export/empty/": "", "export/ä ö.csv": "Ü"})
			old := time.Date(2020, 1, 2, 3, 4, 6, 0, time.UTC)
			_ = os.Chtimes(filepath.Join(dir, "export", "a.csv"), old, old)

			target := filepath.Join(dir, "archive", name)
			out, logs, err := run(t, createDefinition, map[string]any{"source": filepath.Join(dir, "export"), "target": target})
			if err != nil {
				t.Fatal(err)
			}
			o := out.(map[string]any)
			if o["path"] != target || o["files"] != int64(4) || o["size"].(int64) <= 0 {
				t.Errorf("output %v", o)
			}
			if len(logs) != 1 || !strings.HasPrefix(logs[0].Msg, "packed 4 files into ") {
				t.Errorf("logs %v", logs)
			}

			dst := filepath.Join(dir, "unpacked")
			out, _, err = run(t, extractDefinition, map[string]any{"source": target, "target": dst})
			if err != nil {
				t.Fatal(err)
			}
			if n := len(out.([]any)); n != 3 {
				t.Errorf("output %v", out)
			}
			got := contents(t, dst)
			if keys(got) != "export/,export/a.csv,export/empty/,export/sub/,export/sub/b.txt,export/ä ö.csv" ||
				got["export/a.csv"] != "A" || got["export/ä ö.csv"] != "Ü" {
				t.Errorf("unpacked %v", got)
			}
			if info, _ := os.Stat(filepath.Join(dst, "export", "a.csv")); !info.ModTime().Equal(old) {
				t.Errorf("modification time %v, want %v", info.ModTime(), old)
			}
		})
	}
}

func TestCreateWithWildcardsAndOptions(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]string{"out/a.csv": "A", "out/b.csv": "B", "out/c.txt": "C"})
	target := filepath.Join(dir, "out", "csv.data")
	// Without a known ending the format must be set; the archive in the folder is left out.
	if _, _, err := run(t, createDefinition, map[string]any{"source": filepath.Join(dir, "out", "*"), "target": target}); err == nil ||
		!strings.Contains(err.Error(), "has no known ending") {
		t.Errorf("err = %v", err)
	}
	out, _, err := run(t, createDefinition, map[string]any{"source": filepath.Join(dir, "out", "*.csv"), "target": target, "format": "zip"})
	if err != nil || out.(map[string]any)["files"] != int64(2) {
		t.Fatalf("%v, %v", out, err)
	}
	if _, _, err := run(t, createDefinition, map[string]any{"source": filepath.Join(dir, "out", "*.csv"), "target": target, "format": "zip", "ifExists": "fail"}); err == nil ||
		!strings.Contains(err.Error(), "already exists (ifExists: fail)") {
		t.Errorf("err = %v", err)
	}
	out, _, err = run(t, createDefinition, map[string]any{"source": filepath.Join(dir, "out", "*"), "target": target, "format": "zip"})
	if err != nil || out.(map[string]any)["files"] != int64(3) {
		t.Fatalf("archive packed itself: %v, %v", out, err)
	}
	// By content: a ZIP without ending.
	dst := filepath.Join(dir, "in")
	if _, _, err := run(t, extractDefinition, map[string]any{"source": target, "target": dst, "pattern": "*.CSV"}); err != nil {
		t.Fatal(err)
	}
	want := "a.csv,b.csv"
	if os.PathSeparator != '\\' {
		want = "" // the pattern ignores case on Windows only
	}
	if got := keys(contents(t, dst)); got != want {
		t.Errorf("unpacked %q, want %q", got, want)
	}
	if _, _, err := run(t, createDefinition, map[string]any{"source": filepath.Join(dir, "none", "*.csv"), "target": target, "format": "zip"}); err == nil ||
		!strings.Contains(err.Error(), "nothing found") {
		t.Errorf("err = %v", err)
	}
}

func TestConfigErrors(t *testing.T) {
	actiontest.ConfigError(t, createDefinition, map[string]any{"source": "a", "target": "b.zip", "format": "rar"}, `invalid format "rar"`)
	actiontest.ConfigError(t, createDefinition, map[string]any{"source": "a", "target": "b.zip", "ifExists": "skip"}, "expected replace or fail")
	actiontest.ConfigError(t, extractDefinition, map[string]any{"source": "a.zip", "target": "b", "ifExists": "keep"}, "expected fail, replace or skip")
	actiontest.ConfigError(t, extractDefinition, map[string]any{"source": "a.zip", "target": "b", "pattern": "[a"}, "invalid wildcard pattern")
	actiontest.ConfigError(t, extractDefinition, map[string]any{"source": "a.zip", "target": "b", "maxSize": "lots"}, `maxSize: "lots" is not a size`)
	actiontest.ConfigError(t, extractDefinition, map[string]any{"source": "a.zip", "target": "b", "maxFiles": -1}, "must not be negative")
	actiontest.New(t, extractDefinition, map[string]any{"source": "a.zip", "target": "b", "maxSize": "${limit}"})
}

func TestSize(t *testing.T) {
	for text, want := range map[string]int64{"": 7, "1024": 1024, "2KB": 2048, "500 mb": 500 << 20, "10GB": 10 << 30, "3B": 3} {
		if got, err := size("maxSize", text, 7); err != nil || got != want {
			t.Errorf("size(%q) = %d, %v; want %d", text, got, err, want)
		}
	}
	for _, text := range []string{"0", "-1MB", "1TB", "MB", "99999999999999999GB"} {
		if _, err := size("maxSize", text, 7); err == nil {
			t.Errorf("size(%q): no error", text)
		}
	}
}

// zipFile writes a ZIP with the entries (name → content) to path; flags are set on all.
func zipFile(t testing.TB, path string, flags uint16, entries ...[2]string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e[0], Method: zip.Deflate, Flags: flags})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(e[1]))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// tarFile writes a TAR.GZ with the headers (and their content as Linkname of regular files).
func tarFile(t testing.TB, path string, headers ...*tar.Header) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range headers {
		content := ""
		if h.Typeflag == tar.TypeReg {
			content, h.Linkname = h.Linkname, ""
			h.Size = int64(len(content))
		}
		h.Mode = 0o644
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(content))
	}
	_ = tw.Close()
	_ = gz.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExtractRejectsPathsOutsideTheFolder(t *testing.T) {
	for _, name := range []string{"../evil.txt", "a/../../evil.txt", "/etc/evil.txt", `..\evil.txt`, `C:\evil.txt`, "C:evil.txt", `\\server\share\evil.txt`} {
		dir := t.TempDir()
		src := filepath.Join(dir, "bad.zip")
		zipFile(t, src, 0, [2]string{"ok.txt", "fine"}, [2]string{name, "evil"})
		_, _, err := run(t, extractDefinition, map[string]any{"source": src, "target": filepath.Join(dir, "out")})
		if err == nil || !strings.Contains(err.Error(), "would be unpacked outside the folder; nothing is unpacked") {
			t.Errorf("%s: err = %v", name, err)
		}
		// Nothing was written, not even the harmless entry.
		if present(filepath.Join(dir, "out")) || present(filepath.Join(dir, "evil.txt")) {
			t.Errorf("%s: files written", name)
		}
	}
}

func present(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func TestExtractLeavesOutLinks(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "links.tar.gz")
	tarFile(t, src,
		&tar.Header{Name: "data/", Typeflag: tar.TypeDir},
		&tar.Header{Name: "data/a.csv", Typeflag: tar.TypeReg, Linkname: "A"},
		&tar.Header{Name: "data/passwd", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
		&tar.Header{Name: "data/hard", Typeflag: tar.TypeLink, Linkname: "data/a.csv"},
		&tar.Header{Name: "data/fifo", Typeflag: tar.TypeFifo},
	)
	dst := filepath.Join(dir, "out")
	out, logs, err := run(t, extractDefinition, map[string]any{"source": src, "target": dst})
	if err != nil {
		t.Fatal(err)
	}
	if got := keys(contents(t, dst)); got != "data/,data/a.csv" {
		t.Errorf("unpacked %s", got)
	}
	if len(out.([]any)) != 1 || out.([]any)[0] != filepath.Join(dst, "data", "a.csv") {
		t.Errorf("output %v", out)
	}
	if len(logs) != 2 || logs[0].Msg != "3 entries not unpacked: links and special files are left out" {
		t.Errorf("logs %v", logs)
	}
}

func TestExtractLimits(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.zip")
	zipFile(t, src, 0, [2]string{"a.txt", strings.Repeat("x", 3000)}, [2]string{"b.txt", "small"})
	dst := filepath.Join(dir, "out")
	_, _, err := run(t, extractDefinition, map[string]any{"source": src, "target": dst, "maxSize": "2KB"})
	if err == nil || err.Error() != src+" unpacks to more than 2KB; set maxSize if this is intended" {
		t.Errorf("err = %v", err)
	}
	if present(dst) {
		t.Error("files written")
	}
	_, _, err = run(t, extractDefinition, map[string]any{"source": src, "target": dst, "maxFiles": 1})
	if err == nil || !strings.Contains(err.Error(), "has more than 1 entries; set maxFiles") {
		t.Errorf("err = %v", err)
	}
	if _, _, err := run(t, extractDefinition, map[string]any{"source": src, "target": dst, "maxSize": "4KB", "maxFiles": 2}); err != nil {
		t.Error(err)
	}
}

// An archive that states a smaller size than it holds stops the action while unpacking,
// without leaving a temporary file behind.
func TestExtractStopsAtWrongSizes(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.CreateRaw(&zip.FileHeader{Name: "bomb.txt", Method: zip.Store, CompressedSize64: 4000, UncompressedSize64: 10, CRC32: 0})
	_, _ = w.Write(bytes.Repeat([]byte("x"), 4000))
	_ = zw.Close()
	src := filepath.Join(dir, "bomb.zip")
	_ = os.WriteFile(src, buf.Bytes(), 0o644)
	dst := filepath.Join(dir, "out")
	_, _, err := run(t, extractDefinition, map[string]any{"source": src, "target": dst, "maxSize": "1KB"})
	if err == nil || !strings.Contains(err.Error(), `cannot unpack "bomb.txt"`) {
		t.Errorf("err = %v", err)
	}
	if got := keys(contents(t, dst)); got != "" {
		t.Errorf("left behind: %s", got)
	}
}

func TestExtractIfExists(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.zip")
	zipFile(t, src, 0, [2]string{"a.txt", "new A"}, [2]string{"b.txt", "new B"})
	dst := filepath.Join(dir, "out")
	tree(t, dst, map[string]string{"b.txt": "old B"})

	_, _, err := run(t, extractDefinition, map[string]any{"source": src, "target": dst})
	if err == nil || !strings.Contains(err.Error(), `"b.txt" already exists in `+dst+" (set ifExists: replace or skip)") {
		t.Errorf("err = %v", err)
	}
	if got := contents(t, dst); keys(got) != "b.txt" {
		t.Errorf("written before the check: %v", got)
	}
	out, _, err := run(t, extractDefinition, map[string]any{"source": src, "target": dst, "ifExists": "skip"})
	if err != nil || len(out.([]any)) != 1 {
		t.Fatalf("%v, %v", out, err)
	}
	if got := contents(t, dst); got["a.txt"] != "new A" || got["b.txt"] != "old B" {
		t.Errorf("skip: %v", got)
	}
	if _, _, err := run(t, extractDefinition, map[string]any{"source": src, "target": dst, "ifExists": "replace"}); err != nil {
		t.Fatal(err)
	}
	if got := contents(t, dst); got["b.txt"] != "new B" {
		t.Errorf("replace: %v", got)
	}
}

func TestExtractNamesInCodePage437(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "explorer.zip")
	// "Bücher.csv" as Windows Explorer writes it: CP437, without the UTF-8 flag.
	zipFile(t, src, 0, [2]string{"B\x81cher.csv", "x"}, [2]string{"Grüße.csv", "y"})
	// The second name is UTF-8 without the flag: it is read as CP437 too, like other tools do.
	dst := filepath.Join(dir, "out")
	if _, _, err := run(t, extractDefinition, map[string]any{"source": src, "target": dst}); err != nil {
		t.Fatal(err)
	}
	if got := contents(t, dst); got["Bücher.csv"] != "x" {
		t.Errorf("unpacked %v", keys(got))
	}
	// With the flag the name stays UTF-8.
	zipFile(t, src, 0x800, [2]string{"Grüße.csv", "y"})
	dst = filepath.Join(dir, "out2")
	if _, _, err := run(t, extractDefinition, map[string]any{"source": src, "target": dst}); err != nil {
		t.Fatal(err)
	}
	if got := contents(t, dst); got["Grüße.csv"] != "y" {
		t.Errorf("unpacked %v", keys(got))
	}
}

func TestExtractErrors(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]string{"text.txt": "no archive", "broken.zip": "PK\x03\x04 broken", "broken.tar.gz": "\x1f\x8b broken"})
	for name, want := range map[string]string{
		"missing.zip":   `cannot read "`,
		"text.txt":      "not a ZIP, TAR or TAR.GZ archive",
		"broken.zip":    "not a readable ZIP archive",
		"broken.tar.gz": "not a readable TAR.GZ archive",
	} {
		_, _, err := run(t, extractDefinition, map[string]any{"source": filepath.Join(dir, name), "target": filepath.Join(dir, "out")})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", name, err, want)
		}
	}
}

func TestFormatByContent(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]string{"in/a.txt": "A"})
	for _, format := range []string{"zip", "tar", "tar.gz"} {
		src := filepath.Join(dir, format+".bin")
		if _, _, err := run(t, createDefinition, map[string]any{"source": filepath.Join(dir, "in"), "target": src, "format": format}); err != nil {
			t.Fatal(err)
		}
		f, _ := os.Open(src)
		got := byContent(bufio.NewReader(f))
		_ = f.Close()
		if got != format {
			t.Errorf("byContent = %q, want %q", got, format)
		}
		dst := filepath.Join(dir, "out-"+format)
		if _, _, err := run(t, extractDefinition, map[string]any{"source": src, "target": dst}); err != nil || contents(t, dst)["in/a.txt"] != "A" {
			t.Errorf("%s: %v", format, err)
		}
	}
}

func TestCancelled(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]string{"in/a.txt": "A"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, def := range []action.Definition{createDefinition, extractDefinition} {
		a := actiontest.New(t, def, map[string]any{"source": filepath.Join(dir, "in"), "target": filepath.Join(dir, "x.zip")})
		if _, _, err := actiontest.Execute(ctx, a); !errors.Is(err, context.Canceled) {
			t.Errorf("%s: err = %v", def.Type, err)
		}
	}
}

func TestClean(t *testing.T) {
	for name, want := range map[string]string{
		"a.txt": "a.txt", "dir/": "dir", "./a/./b.txt": "a/b.txt", `dir\sub\x.txt`: "dir/sub/x.txt",
		"": "", ".": "", "..": "", "../x": "", "a/../b": "", "/x": "", "C:/x": "", "x:y": "", "a/\xff": "",
	} {
		if got := clean(name); got != want {
			t.Errorf("clean(%q) = %q, want %q", name, got, want)
		}
	}
}

// FuzzExtract unpacks arbitrary bytes as ZIP and TAR.GZ: no panic, and nothing is written
// outside the target folder.
func FuzzExtract(f *testing.F) {
	dir := f.TempDir()
	tree(f, dir, map[string]string{"in/a.txt": "A", "in/sub/b.txt": "B"})
	for _, name := range []string{"seed.zip", "seed.tar.gz", "seed.tar"} {
		src := filepath.Join(dir, name)
		if _, _, err := run(f, createDefinition, map[string]any{"source": filepath.Join(dir, "in"), "target": src}); err != nil {
			f.Fatal(err)
		}
		b, _ := os.ReadFile(src)
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		src := filepath.Join(dir, "fuzz.bin")
		if err := os.WriteFile(src, data, 0o644); err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(dir, "box", "out")
		a := actiontest.New(t, extractDefinition, map[string]any{"source": src, "target": dst, "maxSize": "1MB", "maxFiles": 100})
		_, _, _ = actiontest.Execute(context.Background(), a)
		got := contents(t, dir)
		for name := range got {
			if name != "fuzz.bin" && name != "box/" && name != "box/out/" && !strings.HasPrefix(name, "box/out/") {
				t.Fatalf("written outside the folder: %s", name)
			}
		}
	})
}

func BenchmarkExtract(b *testing.B) {
	dir := b.TempDir()
	files := map[string]string{}
	for i := range 200 {
		files["in/"+strings.Repeat("d", i%5)+"/f"+string(rune('a'+i%26))+".csv"] = strings.Repeat("1;2;3\n", 500)
	}
	tree(b, dir, files)
	for _, name := range []string{"a.zip", "a.tar.gz"} {
		src := filepath.Join(dir, name)
		if _, _, err := run(b, createDefinition, map[string]any{"source": filepath.Join(dir, "in"), "target": src}); err != nil {
			b.Fatal(err)
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := range b.N {
				dst := filepath.Join(dir, "out", name, strings.Repeat("x", i%3)+string(rune('a'+i%26)))
				if _, _, err := run(b, extractDefinition, map[string]any{"source": src, "target": dst, "ifExists": "replace"}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCreate(b *testing.B) {
	dir := b.TempDir()
	files := map[string]string{}
	for i := range 100 {
		files["in/sub"+string(rune('a'+i%10))+"/f"+string(rune('a'+i%26))+".csv"] = strings.Repeat("1;2;3\n", 500)
	}
	tree(b, dir, files)
	b.ReportAllocs()
	for range b.N {
		if _, _, err := run(b, createDefinition, map[string]any{"source": filepath.Join(dir, "in"), "target": filepath.Join(dir, "a.zip")}); err != nil {
			b.Fatal(err)
		}
	}
}
