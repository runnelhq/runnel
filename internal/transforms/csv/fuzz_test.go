package csv

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

// FuzzInput checks that arbitrary file contents and encodings never make csv.input
// panic: broken quotes, other column counts, byte order marks, invalid UTF-8 or UTF-16.
func FuzzInput(f *testing.F) {
	files, _ := filepath.Glob(filepath.Join("..", "..", "..", "examples", "*", "data", "*.csv"))
	for _, file := range files {
		if data, err := os.ReadFile(file); err == nil {
			f.Add(data, uint8(0))
		}
	}
	f.Add([]byte("\xEF\xBB\xBFa;b\n1;\"x\"\"y\"\n"), uint8(1))
	f.Add([]byte("\xFF\xFEa\x00;\x00b\x00\n\x00"), uint8(3))
	encodings := []string{"auto", "utf-8", "windows-1252", "utf-16"}
	f.Fuzz(func(t *testing.T, data []byte, enc uint8) {
		path := filepath.Join(t.TempDir(), "in.csv")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		tr := transformtest.New(t, inputDefinition, map[string]any{
			"path": path, "delimiter": ";", "encoding": encodings[int(enc)%len(encodings)],
		})
		_, _, _ = transformtest.Run(context.Background(), tr)
	})
}

// FuzzDetectFields checks that "Detect fields" (S46) never panics on arbitrary files and
// always proposes a valid type and format for every column.
func FuzzDetectFields(f *testing.F) {
	files, _ := filepath.Glob(filepath.Join("..", "..", "..", "examples", "*", "data", "*.csv"))
	for _, file := range files {
		if data, err := os.ReadFile(file); err == nil {
			f.Add(data, false)
		}
	}
	f.Add([]byte("a;b\n0221;1.234,5\n;19.11.2026\n"), true)
	f.Fuzz(func(t *testing.T, data []byte, noHeader bool) {
		path := filepath.Join(t.TempDir(), "in.csv")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		d, err := detectFields(map[string]any{"path": path, "delimiter": ";", "header": !noHeader}, 50)
		if err != nil {
			return
		}
		for _, fd := range d.Fields {
			if _, err := format.New(record.Type(fd.Type), fd.Format); err != nil {
				t.Errorf("field %q: invalid proposal %s %q: %v", fd.Name, fd.Type, fd.Format, err)
			}
		}
	})
}
