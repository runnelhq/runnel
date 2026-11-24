package file

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/httpx"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/textenc"
)

// file.read reads a small text file into the workflow (S66), e.g. a configuration in
// JSON, a token or a marker of the last run: as text, as lines or as a JSON value. Large
// files belong into a pipeline (csv.input, json.input). Output: content (and value or
// lines), size and modified.
var readDefinition = action.Definition{
	Type:        "file.read",
	Category:    action.CategoryFiles,
	Label:       "Read File",
	Description: "Reads a small text file into the workflow, as text, lines or JSON; output: content, value or lines, size and modified",
	Example:     "path: config/customers.json\nformat: json\n",
	Config:      readFile{},
	New:         newReadFile,
	Renamed:     map[string]string{"as": "renamed to format"},
}

type readFile struct {
	Path      string `json:"path" doc:"File to read, relative to the project folder or absolute" schema:"required,example=config/settings.json"`
	Format    string `json:"format" doc:"How the content is read: text, lines (a list without line ends) or json (a value, e.g. {{ results.read.output.value.customers }})" schema:"enum=text|lines|json,default=text"`
	Encoding  string `json:"encoding" doc:"Encoding of the file; auto recognizes UTF-8 with or without byte order mark and UTF-16" schema:"enum=auto|utf-8|windows-1252|utf-16,default=auto"`
	IfMissing string `json:"ifMissing" doc:"If the file does not exist: fail, or go on with empty content (output missing: true)" schema:"enum=fail|empty,default=fail"`
	MaxSize   string `json:"maxSize" doc:"Largest file to read, e.g. 500KB or 10MB; the content goes into the result of the run" schema:"default=10MB"`
}

const defaultReadMax = 10 << 20

func newReadFile(with map[string]any) (action.Action, error) {
	a := &readFile{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	switch a.Format {
	case "", "text", "lines", "json":
	default:
		return nil, fmt.Errorf("invalid format %q (expected text, lines or json)", a.Format)
	}
	switch a.IfMissing {
	case "", "fail", "empty":
	default:
		return nil, fmt.Errorf("invalid ifMissing %q (expected fail or empty)", a.IfMissing)
	}
	if _, err := textenc.Decoder(strings.NewReader(""), a.Encoding); err != nil {
		return nil, err
	}
	if !strings.Contains(a.MaxSize, "{{") && !strings.Contains(a.MaxSize, "${") {
		if _, err := parseSize(a.MaxSize, defaultReadMax); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *readFile) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	if a.Path == "" {
		return action.Result{}, errors.New("no path given")
	}
	limit, err := parseSize(a.MaxSize, defaultReadMax)
	if err != nil {
		return action.Result{}, err
	}
	name := paths.Resolve(ctx, a.Path)
	f, err := os.Open(name)
	if errors.Is(err, fs.ErrNotExist) && a.IfMissing == "empty" {
		ec.Logf(slog.LevelInfo, "%s does not exist, going on with empty content", a.Path)
		out := map[string]any{"content": "", "size": int64(0), "missing": true}
		switch a.Format {
		case "lines":
			out["lines"] = []any{}
		case "json":
			out["value"] = nil
		}
		return action.Result{Output: out}, nil
	}
	if err != nil {
		return action.Result{}, fmt.Errorf("cannot read %q: %w", a.Path, plain(err))
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return action.Result{}, fmt.Errorf("cannot read %q: %w", a.Path, plain(err))
	}
	if info.IsDir() {
		return action.Result{}, fmt.Errorf("cannot read %q: it is a folder", a.Path)
	}
	if info.Size() > limit {
		return action.Result{}, fmt.Errorf("%s is larger than %s (%d bytes); set maxSize if this is intended, or read it in a pipeline", a.Path, sizeOr(a.MaxSize, "10MB"), info.Size())
	}
	r, err := textenc.Decoder(io.LimitReader(f, limit+1), a.Encoding)
	if err != nil {
		return action.Result{}, err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return action.Result{}, fmt.Errorf("cannot read %q: %w", a.Path, plain(err))
	}
	text := string(data)
	out := map[string]any{"content": text, "size": info.Size(), "modified": info.ModTime()}
	switch a.Format {
	case "lines":
		out["lines"] = lines(text)
	case "json":
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return action.Result{}, fmt.Errorf("%s is not valid JSON: %s", a.Path, jsonError(data, err))
		}
		if dec.More() {
			return action.Result{}, fmt.Errorf("%s is not valid JSON: more than one value", a.Path)
		}
		out["value"] = httpx.Numbers(v)
	}
	ec.Logf(slog.LevelDebug, "read %s (%d bytes)", a.Path, info.Size())
	return action.Result{Output: out}, nil
}

// lines splits a text into its lines, without \n or \r\n; a line end at the end of the
// text does not make an empty last line.
func lines(text string) []any {
	text = strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if text == "" {
		return []any{}
	}
	parts := strings.Split(text, "\n")
	out := make([]any, len(parts))
	for i, p := range parts {
		out[i] = p
	}
	return out
}

// jsonError names line and column of a syntax error.
func jsonError(data []byte, err error) string {
	if se, ok := errors.AsType[*json.SyntaxError](err); ok {
		before := data[:min(max(int(se.Offset)-1, 0), len(data))]
		line := bytes.Count(before, []byte("\n")) + 1
		col := len(before) - bytes.LastIndexByte(before, '\n')
		return fmt.Sprintf("line %d, column %d: %s", line, col, se.Error())
	}
	return err.Error()
}
