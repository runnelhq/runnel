package json

import (
	"bufio"
	"context"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/textenc"
	"github.com/runnelhq/runnel/internal/transform"
)

// json.input reads the records of a JSON or NDJSON file: the entries of an array (also
// one inside the document, records:), single objects, or one object per line.
var inputDefinition = transform.Definition{
	Type:         "json.input",
	RecordErrors: true,
	Category:     transform.CategoryInput,
	Label:        "JSON Input",
	Description:  "Reads the objects of a JSON or NDJSON file as records",
	Example:      "path: data/orders.json\nrecords: data.items\nfields:\n  - { name: id, type: int }\n  - { name: city, from: address.city }\n",
	Config:       input{},
	New:          newInput,
	DetectFields: detectFields,
}

type input struct {
	Path     string  `json:"path" doc:"JSON or NDJSON file to read, relative to the project folder or absolute" schema:"required,example=data/orders.json"`
	Format   string  `json:"format" doc:"json: arrays and objects, also several one after another; ndjson: one object per line, a broken line goes to the error link; auto: ndjson for .ndjson and .jsonl, else json" schema:"enum=auto|json|ndjson,default=auto"`
	Records  string  `json:"records" doc:"Path to the array of records inside the document, with dots, e.g. data.items; empty: the document itself (json only)" schema:"example=data.items"`
	Encoding string  `json:"encoding" doc:"Encoding of the file; auto recognizes UTF-8 and UTF-16 by their byte order mark and reads UTF-8 otherwise" schema:"enum=auto|utf-8|windows-1252|utf-16,default=auto"`
	Fields   []field `json:"fields" doc:"Fields to read, with path and type; empty: all keys of the objects as they are"`

	codecs []format.Codec
}

func newInput(with map[string]any) (transform.Transform, error) {
	t := &input{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	switch t.Format {
	case "", formatAuto, formatJSON, formatNDJSON:
	default:
		return nil, fmt.Errorf("invalid format %q (expected auto, json or ndjson)", t.Format)
	}
	if t.Format == formatNDJSON && t.Records != "" {
		return nil, errors.New("records: only for json; in ndjson every line is a record")
	}
	seen := map[string]bool{}
	for _, f := range t.Fields {
		if f.Name == "" {
			return nil, errors.New("fields: every field needs a name")
		}
		if seen[f.Name] {
			return nil, fmt.Errorf("fields: the field %q is listed twice", f.Name)
		}
		seen[f.Name] = true
		typ := record.Type(f.Type)
		if f.Type == "" {
			typ = record.Unknown
		}
		c, err := format.New(typ, f.Format)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", f.Name, err)
		}
		if f.Type == "" {
			c = format.Codec{} // keep the value as it is
		}
		t.codecs = append(t.codecs, c)
	}
	return t, nil
}

// Init reports the schema: with fields closed, with their types (as in the file when
// no type is given); without them open, as the keys are only known when reading.
func (t *input) Init(*transform.InitContext) (record.Schema, error) {
	if len(t.Fields) == 0 {
		return record.Schema{Open: true}, nil
	}
	var s record.Schema
	for i, f := range t.Fields {
		typ := record.Unknown
		if f.Type != "" {
			typ = t.codecs[i].Type()
		}
		s = s.With(f.Name, typ)
	}
	return s, nil
}

// mode returns the format to read: ndjson for .ndjson and .jsonl with auto.
func (t *input) mode() string {
	if t.Format == "" || t.Format == formatAuto {
		switch strings.ToLower(filepath.Ext(t.Path)) {
		case ".ndjson", ".jsonl":
			return formatNDJSON
		}
		return formatJSON
	}
	return t.Format
}

// item is a record read, or a line that could not be read (ndjson).
type item struct {
	r    record.Record
	line int // ndjson: line of the record
	err  error
}

// read reads the records of the file and calls each for every one; each returning
// false ends reading.
func (t *input) read(ctx context.Context, each func(item) (bool, error)) error {
	file := paths.Resolve(ctx, t.Path)
	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("cannot read %q: %w", t.Path, plain(err))
	}
	defer func() { _ = f.Close() }()
	text, err := textenc.Decoder(f, t.Encoding)
	if err != nil {
		return err
	}
	if t.mode() == formatNDJSON {
		return t.readLines(text, each)
	}
	dec := stdjson.NewDecoder(text)
	dec.UseNumber()
	err = t.readValues(dec, each)
	if err == nil || errors.Is(err, errStop) {
		return nil
	}
	if errors.Is(err, textenc.ErrNotUTF8) {
		return fmt.Errorf("%s: %w", t.Path, err)
	}
	if stop, ok := errors.AsType[stopError](err); ok {
		return stop.err
	}
	return fmt.Errorf("%s: %s", t.where(file, dec.InputOffset()), syntaxMessage(err))
}

var errStop = errors.New("stop")

// stopError is an error of each, passed on as it is.
type stopError struct{ err error }

func (e stopError) Error() string { return e.err.Error() }

// readValues reads one value after another: an array gives its entries, an object one
// record; records: first leads to the array inside the document.
func (t *input) readValues(dec *stdjson.Decoder, each func(item) (bool, error)) error {
	call := func(r record.Record) error {
		more, err := each(item{r: r})
		if err != nil {
			return stopError{err}
		}
		if !more {
			return errStop
		}
		return nil
	}
	for first := true; ; first = false {
		if first && t.Records != "" {
			if err := seek(dec, t.Records); err != nil {
				return err
			}
		}
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil // also an empty file: no records
		}
		if err != nil {
			return err
		}
		if d, ok := tok.(stdjson.Delim); ok && d == '[' {
			for dec.More() {
				r, err := readObject(dec)
				if err != nil {
					return err
				}
				if err := call(r); err != nil {
					return err
				}
			}
			if _, err := dec.Token(); err != nil {
				return err
			}
		} else {
			r, err := objectFrom(dec, tok)
			if err != nil {
				return err
			}
			if err := call(r); err != nil {
				return err
			}
		}
		if t.Records != "" {
			return nil // the rest of the document is not read
		}
	}
}

// objectFrom reads a record whose first token was read already: an object keeps the
// order of its keys; any other value becomes the field "value".
func objectFrom(dec *stdjson.Decoder, tok stdjson.Token) (record.Record, error) {
	if d, ok := tok.(stdjson.Delim); ok && d == '{' {
		var fields []record.Field
		seen := map[string]bool{}
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return record.Record{}, err
			}
			key := fmt.Sprint(k)
			var raw any
			if err := dec.Decode(&raw); err != nil {
				return record.Record{}, err
			}
			v, err := value(raw)
			if err != nil {
				return record.Record{}, fmt.Errorf("key %q: %w", key, err)
			}
			if seen[key] {
				return record.Record{}, stopError{fmt.Errorf("the key %q appears twice in an object", key)}
			}
			seen[key] = true
			fields = append(fields, record.Field{Name: key, Value: v})
		}
		if _, err := dec.Token(); err != nil {
			return record.Record{}, err
		}
		return record.FromFields(fields), nil
	}
	v, err := rest(dec, tok)
	if err != nil {
		return record.Record{}, err
	}
	return record.New(record.Field{Name: "value", Value: v}), nil
}

// seek moves dec to the value at a path of keys (data.items) in the first document.
func seek(dec *stdjson.Decoder, path string) error {
	for key := range strings.SplitSeq(path, ".") {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(stdjson.Delim); !ok || d != '{' {
			return stopError{fmt.Errorf("records: %q is not inside an object", key)}
		}
		found := false
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return err
			}
			if k == key {
				found = true
				break
			}
			var skip stdjson.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return err
			}
		}
		if !found {
			return stopError{fmt.Errorf("records: the document has no key %q on the path %q", key, path)}
		}
	}
	return nil
}

// readLines reads ndjson: one value per line; empty lines are skipped.
func (t *input) readLines(text io.Reader, each func(item) (bool, error)) error {
	sc := bufio.NewScanner(text)
	sc.Buffer(make([]byte, 64*1024), maxLine)
	for line := 1; sc.Scan(); line++ {
		s := strings.TrimSpace(sc.Text())
		if s == "" {
			continue
		}
		dec := stdjson.NewDecoder(strings.NewReader(s))
		dec.UseNumber()
		r, err := readObject(dec)
		if err == nil && dec.More() {
			err = errors.New("more than one value in the line")
		}
		it := item{r: r, line: line}
		if err != nil {
			it = item{r: record.New(record.Field{Name: "line", Value: s}), line: line,
				err: fmt.Errorf("%s:%d: %s", t.Path, line, syntaxMessage(err))}
		}
		more, err := each(it)
		if err != nil || !more {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return fmt.Errorf("%s: a line is longer than %d MiB", t.Path, maxLine>>20)
		}
		return fmt.Errorf("%s: %w", t.Path, plain(err))
	}
	return nil
}

// maxLine limits a line of ndjson.
const maxLine = 64 << 20

// where names the place of a syntax error: line and column for UTF-8 files.
func (t *input) where(file string, offset int64) string {
	f, err := os.Open(file)
	if err != nil {
		return t.Path
	}
	defer func() { _ = f.Close() }()
	text, err := textenc.Decoder(f, t.Encoding)
	if err != nil {
		return t.Path
	}
	data, _ := io.ReadAll(io.LimitReader(text, offset))
	line, col := position(data, offset)
	return fmt.Sprintf("%s:%d:%d", t.Path, line, col)
}

func (t *input) Process(ctx context.Context, _ <-chan transform.Batch, out *transform.Output) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var n, failed int64
	err := t.read(ctx, func(it item) (bool, error) {
		n++
		if it.err != nil {
			failed++
			return true, out.Fail(it.r, "", it.err)
		}
		r, badField, err := t.selectFields(it.r)
		if err != nil {
			failed++
			at := t.Path
			if it.line > 0 {
				at = fmt.Sprintf("%s:%d", t.Path, it.line)
			}
			return true, out.Fail(it.r, badField, fmt.Errorf("%s: record %d, field %q: %w", at, n, badField, err))
		}
		return true, out.Emit(r)
	})
	if err != nil {
		return err
	}
	if failed > 0 {
		out.Logf(levelInfo, "read %d %s from %s, %d with errors", n, plural(n, "record"), t.Path, failed)
	} else {
		out.Logf(levelInfo, "read %d %s from %s", n, plural(n, "record"), t.Path)
	}
	return nil
}

// selectFields keeps the fields of the configuration, in its order and converted to
// their types; without fields the record stays as it is.
func (t *input) selectFields(r record.Record) (record.Record, string, error) {
	if len(t.Fields) == 0 {
		return r, "", nil
	}
	fields := make([]record.Field, len(t.Fields))
	for i, f := range t.Fields {
		from := f.From
		if from == "" {
			from = f.Name
		}
		v, _ := lookup(r, from)
		if f.Type != "" {
			c, err := t.codecs[i].Convert(v)
			if err != nil {
				return record.Record{}, f.Name, err
			}
			v = c
		}
		fields[i] = record.Field{Name: f.Name, Value: v}
	}
	return record.FromFields(fields), "", nil
}
