package file

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

// output runs an action and returns its output map.
func output(t *testing.T, def action.Definition, with map[string]any) (map[string]any, error) {
	t.Helper()
	res, _, err := actiontest.Execute(context.Background(), actiontest.New(t, def, with))
	out, _ := res.Output.(map[string]any)
	return out, err
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]string{
		"config.json": `{"customers": [4711, 4712], "rate": 0.19}`,
		"list.txt":    "a\r\nb\n\nc\n",
		"bad.json":    "{\n  \"a\": 1,\n  \"b\": \n}",
	})
	// UTF-8 with byte order mark and Windows-1252 (ä = 0xE4).
	if err := os.WriteFile(filepath.Join(dir, "bom.txt"), []byte("\xEF\xBB\xBFGrüße"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ansi.txt"), []byte("M\xE4rz"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := output(t, readDefinition, map[string]any{"path": filepath.Join(dir, "config.json"), "as": "json"})
	value, _ := out["value"].(map[string]any)
	if err != nil || len(value["customers"].([]any)) != 2 || value["customers"].([]any)[0] != int64(4711) || out["size"] != int64(41) {
		t.Fatalf("json: %v %v", out, err)
	}
	if value["rate"] != 0.19 {
		t.Errorf("rate %T %v", value["rate"], value["rate"])
	}
	if out, err := output(t, readDefinition, map[string]any{"path": filepath.Join(dir, "list.txt"), "as": "lines"}); err != nil || len(out["lines"].([]any)) != 4 || out["lines"].([]any)[2] != "" || out["lines"].([]any)[3] != "c" {
		t.Errorf("lines: %v %v", out, err)
	}
	if out, err := output(t, readDefinition, map[string]any{"path": filepath.Join(dir, "bom.txt")}); err != nil || out["content"] != "Grüße" {
		t.Errorf("bom: %q %v", out["content"], err)
	}
	if out, err := output(t, readDefinition, map[string]any{"path": filepath.Join(dir, "ansi.txt"), "encoding": "windows-1252"}); err != nil || out["content"] != "März" {
		t.Errorf("windows-1252: %q %v", out["content"], err)
	}
	if _, err := output(t, readDefinition, map[string]any{"path": filepath.Join(dir, "ansi.txt")}); err == nil || !strings.Contains(err.Error(), "windows-1252") {
		t.Errorf("not utf-8: %v", err)
	}
	if _, err := output(t, readDefinition, map[string]any{"path": filepath.Join(dir, "bad.json"), "as": "json"}); err == nil || !strings.Contains(err.Error(), "line 4, column 1") {
		t.Errorf("bad json: %v", err)
	}
	// Missing: an error, or empty content with ifMissing: empty.
	if _, err := output(t, readDefinition, map[string]any{"path": filepath.Join(dir, "gone.txt")}); err == nil || !strings.Contains(err.Error(), "cannot read") {
		t.Errorf("missing: %v", err)
	}
	if out, err := output(t, readDefinition, map[string]any{"path": filepath.Join(dir, "gone.json"), "as": "json", "ifMissing": "empty"}); err != nil || out["missing"] != true || out["value"] != nil {
		t.Errorf("missing empty: %v %v", out, err)
	}
	// Larger than maxSize, a folder.
	if _, err := output(t, readDefinition, map[string]any{"path": filepath.Join(dir, "list.txt"), "maxSize": "5B"}); err == nil || !strings.Contains(err.Error(), "larger than 5B") {
		t.Errorf("maxSize: %v", err)
	}
	if _, err := output(t, readDefinition, map[string]any{"path": dir}); err == nil || !strings.Contains(err.Error(), "it is a folder") {
		t.Errorf("folder: %v", err)
	}
	actiontest.ConfigError(t, readDefinition, map[string]any{"path": "a", "as": "xml"}, `invalid as "xml"`)
	actiontest.ConfigError(t, readDefinition, map[string]any{"path": "a", "encoding": "latin9"}, "invalid encoding")
	actiontest.ConfigError(t, readDefinition, map[string]any{"path": "a", "maxSize": "huge"}, "is not a size")
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out", "status.txt")
	read := func() string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	out, err := output(t, writeDefinition, map[string]any{"path": path, "content": "line 1\nline 2\n", "lineEnds": "crlf"})
	if err != nil || read() != "line 1\r\nline 2\r\n" || out["size"] != int64(16) || out["path"] != path {
		t.Fatalf("write: %q %v %v", read(), out, err)
	}
	// append adds; fail refuses an existing file; replace leaves no temporary file.
	if _, err := output(t, writeDefinition, map[string]any{"path": path, "content": "line 3\r\n", "ifExists": "append"}); err != nil || read() != "line 1\r\nline 2\r\nline 3\r\n" {
		t.Errorf("append: %q %v", read(), err)
	}
	if _, err := output(t, writeDefinition, map[string]any{"path": path, "content": "x", "ifExists": "fail"}); err == nil || !strings.Contains(err.Error(), "exists (ifExists: fail)") {
		t.Errorf("fail: %v", err)
	}
	if _, err := output(t, writeDefinition, map[string]any{"path": path, "content": "new"}); err != nil || read() != "new" {
		t.Errorf("replace: %q %v", read(), err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("files left: %v", entries)
	}

	// JSON with exact numbers; a value other than text is JSON even with as: text.
	value := map[string]any{"run": "01J", "rows": int64(3), "total": decimal.RequireFromString("1234.50"), "names": []any{"Anna", "Bäcker & Söhne"}}
	if _, err := output(t, writeDefinition, map[string]any{"path": path, "content": value, "as": "json"}); err != nil ||
		read() != "{\n  \"names\": [\n    \"Anna\",\n    \"Bäcker & Söhne\"\n  ],\n  \"rows\": 3,\n  \"run\": \"01J\",\n  \"total\": 1234.5\n}\n" {
		t.Errorf("json: %q %v", read(), err)
	}
	if _, err := output(t, writeDefinition, map[string]any{"path": path, "content": []any{int64(1), int64(2)}}); err != nil || read() != "[\n  1,\n  2\n]\n" {
		t.Errorf("list as text: %q %v", read(), err)
	}

	// Encodings: byte order mark only at the start, also when appending.
	if _, err := output(t, writeDefinition, map[string]any{"path": path, "content": "Grüße", "encoding": "utf-8-bom"}); err != nil || read() != "\xEF\xBB\xBFGrüße" {
		t.Errorf("utf-8-bom: %q %v", read(), err)
	}
	if _, err := output(t, writeDefinition, map[string]any{"path": path, "content": "!", "encoding": "utf-8-bom", "ifExists": "append"}); err != nil || read() != "\xEF\xBB\xBFGrüße!" {
		t.Errorf("utf-8-bom append: %q %v", read(), err)
	}
	if _, err := output(t, writeDefinition, map[string]any{"path": path, "content": "März", "encoding": "windows-1252"}); err != nil || read() != "M\xE4rz" {
		t.Errorf("windows-1252: %q %v", read(), err)
	}
	if _, err := output(t, writeDefinition, map[string]any{"path": path, "content": "€ ✓", "encoding": "windows-1252"}); err == nil || !strings.Contains(err.Error(), "cannot hold") {
		t.Errorf("windows-1252 refused: %v", err)
	}
	if _, err := output(t, writeDefinition, map[string]any{"path": path, "content": "ä", "encoding": "utf-16le"}); err != nil || read() != "\xFF\xFE\xE4\x00" {
		t.Errorf("utf-16le: %q %v", read(), err)
	}
	actiontest.ConfigError(t, writeDefinition, map[string]any{"path": "a", "ifExists": "keep"}, `invalid ifExists "keep"`)
	actiontest.ConfigError(t, writeDefinition, map[string]any{"path": "a", "encoding": "ascii"}, `invalid encoding "ascii"`)
}
