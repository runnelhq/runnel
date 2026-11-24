package file

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/record"
)

// file.write writes a text or a value as JSON to a file (S66), e.g. a marker for another
// system, a small report or an own log with ifExists: append. A replaced file is written
// to a temporary file first and renamed, so that no one reads half a file. Output: path
// and size.
var writeDefinition = action.Definition{
	Type:        "file.write",
	Category:    action.CategoryFiles,
	Label:       "Write File",
	Description: "Writes a text or a value as JSON to a file, replacing it or appending; output: path and size",
	Example:     "path: out/status.json\nformat: json\ncontent: \"{{ {'run': run.id, 'ok': true} }}\"\n",
	Config:      writeFile{},
	New:         newWriteFile,
}

type writeFile struct {
	Path     string `json:"path" doc:"File to write, relative to the project folder or absolute; missing folders are created" schema:"required,example=out/status.txt"`
	Content  any    `json:"content" doc:"What to write: a text, or with format: json any value, e.g. {{ results.query.output.rows }}"`
	Format   string `json:"format" doc:"text writes the content as text (other values as JSON), json writes the value as formatted JSON" schema:"enum=text|json,default=text"`
	Encoding string `json:"encoding" doc:"Encoding of the file; utf-8-bom lets Excel and older Windows programs recognize umlauts" schema:"enum=utf-8|utf-8-bom|windows-1252|utf-16le,default=utf-8"`
	LineEnds string `json:"lineEnds" doc:"Line ends: as in the content, crlf (Windows) or lf" schema:"enum=keep|crlf|lf,default=keep"`
	IfExists string `json:"ifExists" doc:"If the file exists: replace it once the content is written, append to it, or fail" schema:"enum=replace|append|fail,default=replace"`
}

func newWriteFile(with map[string]any) (action.Action, error) {
	a := &writeFile{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	for _, c := range []struct{ field, value, allowed string }{
		{"format", a.Format, "text|json"}, {"encoding", a.Encoding, "utf-8|utf-8-bom|windows-1252|utf-16le"},
		{"lineEnds", a.LineEnds, "keep|crlf|lf"}, {"ifExists", a.IfExists, "replace|append|fail"},
	} {
		if c.value != "" && !strings.Contains("|"+c.allowed+"|", "|"+c.value+"|") {
			return nil, fmt.Errorf("invalid %s %q (expected %s)", c.field, c.value, strings.ReplaceAll(c.allowed, "|", ", "))
		}
	}
	return a, nil
}

func (a *writeFile) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	if a.Path == "" {
		return action.Result{}, errors.New("no path given")
	}
	text, err := a.text()
	if err != nil {
		return action.Result{}, err
	}
	switch a.LineEnds {
	case "crlf":
		text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
	case "lf":
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}
	name := paths.Resolve(ctx, a.Path)
	_, err = os.Stat(name)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return action.Result{}, fmt.Errorf("cannot write %q: %w", a.Path, plain(err))
	}
	if exists && a.IfExists == "fail" {
		return action.Result{}, fmt.Errorf("%s exists (ifExists: fail)", a.Path)
	}
	appending := exists && a.IfExists == "append"
	// A byte order mark only at the start of the file, not in the middle when appending.
	data, err := encode(text, a.Encoding, !appending)
	if err != nil {
		return action.Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return action.Result{}, fmt.Errorf("cannot write %q: %w", a.Path, plain(err))
	}
	if appending {
		f, err := os.OpenFile(name, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			return action.Result{}, fmt.Errorf("cannot write %q: %w", a.Path, plain(err))
		}
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return action.Result{}, fmt.Errorf("cannot write %q: %w", a.Path, plain(err))
		}
	} else if err := replaceFile(name, data); err != nil {
		return action.Result{}, fmt.Errorf("cannot write %q: %w", a.Path, plain(err))
	}
	verb := map[bool]string{true: "appended", false: "wrote"}[appending]
	ec.Logf(slog.LevelDebug, "%s %d bytes to %s", verb, len(data), a.Path)
	return action.Result{Output: map[string]any{"path": name, "size": int64(len(data))}}, nil
}

// text is the content as it is written: a text as it is; with format: json (and for other
// values than text) formatted JSON with exact numbers.
func (a *writeFile) text() (string, error) {
	if s, ok := a.Content.(string); ok && a.Format != "json" {
		return s, nil
	}
	if a.Content == nil && a.Format != "json" {
		return "", nil
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(record.JSONValue(a.Content)); err != nil {
		return "", fmt.Errorf("content cannot be written as JSON: %w", err)
	}
	return b.String(), nil
}

// encode converts the text into the bytes of the encoding; bom writes a byte order mark
// where the encoding has one (utf-8-bom, utf-16le).
func encode(text, enc string, bom bool) ([]byte, error) {
	switch enc {
	case "", "utf-8":
		return []byte(text), nil
	case "utf-8-bom":
		if bom {
			return append([]byte{0xEF, 0xBB, 0xBF}, text...), nil
		}
		return []byte(text), nil
	case "windows-1252":
		out, err := charmap.Windows1252.NewEncoder().String(text)
		if err != nil {
			return nil, errors.New("the content has characters that windows-1252 cannot hold; use encoding: utf-8-bom")
		}
		return []byte(out), nil
	case "utf-16le":
		policy := unicode.IgnoreBOM
		if bom {
			policy = unicode.UseBOM
		}
		out, err := unicode.UTF16(unicode.LittleEndian, policy).NewEncoder().String(text)
		return []byte(out), err
	}
	return nil, fmt.Errorf("invalid encoding %q", enc)
}

// replaceFile writes data to a temporary file next to name and renames it, so that
// readers never see half a file.
func replaceFile(name string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(name), "."+filepath.Base(name)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), name)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}
