package json

import (
	"bufio"
	"bytes"
	"context"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// json.output writes records to a JSON file (an array, one record per line) or an NDJSON
// file and passes them on. A new file replaces the old one only once all records are
// written.
var outputDefinition = transform.Definition{
	Type:         "json.output",
	RecordErrors: true,
	Category:     transform.CategoryOutput,
	Label:        "JSON Output",
	Description:  "Writes records to a JSON or NDJSON file and passes them on",
	Example:      "path: out/orders.json\nfields:\n  - { name: id }\n  - { name: day, format: dd.MM.yyyy }\n",
	Config:       output{},
	New:          newOutput,
}

type output struct {
	Path     string     `json:"path" doc:"JSON or NDJSON file to write, relative to the project folder or absolute" schema:"required,example=out/orders.json"`
	Format   string     `json:"format" doc:"json: an array with one record per line; ndjson: one object per line; auto: ndjson for .ndjson and .jsonl, else json" schema:"enum=auto|json|ndjson,default=auto"`
	IfExists string     `json:"ifExists" doc:"If the file exists: replace it once all records are written, or fail" schema:"enum=replace|fail,default=replace"`
	Fields   []outField `json:"fields" doc:"Fields to write, in this order; empty: all fields of the records"`

	codecs []*format.Codec
	tree   []*keyNode // keys of the objects, with fields
}

type outField struct {
	Name   string `json:"name" doc:"Name of the field to write" schema:"required,format=runnel-field,example=amount"`
	As     string `json:"as" doc:"Key in the object, with dots for nested objects, e.g. address.city; empty: the name of the field" schema:"example=address.city"`
	Format string `json:"format" doc:"Writes the value as text with a number pattern like #.##0,00 or a date pattern like dd.MM.yyyy; empty: numbers as numbers, dates in ISO 8601" schema:"example=dd.MM.yyyy"`
}

func newOutput(with map[string]any) (transform.Transform, error) {
	t := &output{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	switch t.Format {
	case "", formatAuto, formatJSON, formatNDJSON:
	default:
		return nil, fmt.Errorf("invalid format %q (expected auto, json or ndjson)", t.Format)
	}
	switch t.IfExists {
	case "", "replace", "fail":
	default:
		return nil, fmt.Errorf("invalid ifExists %q (expected replace or fail)", t.IfExists)
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
		var c *format.Codec
		if f.Format != "" {
			tc, err := format.ToText(f.Format)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", f.Name, err)
			}
			c = &tc
		}
		t.codecs = append(t.codecs, c)
	}
	tree, err := keyTree(t.Fields)
	if err != nil {
		return nil, err
	}
	t.tree = tree
	return t, nil
}

// keyNode is a key of an object written by json.output: a field (index into fields), or
// a nested object with its keys in the order of the fields.
type keyNode struct {
	key      string
	field    int
	children []*keyNode
}

// keyTree builds the keys from fields[].as: fields sharing a prefix share one object.
func keyTree(fields []outField) ([]*keyNode, error) {
	var root []*keyNode
	for i, f := range fields {
		path := f.Name
		if f.As != "" {
			path = f.As
		}
		parts := []string{path}
		if f.As != "" {
			parts = strings.Split(f.As, ".")
		}
		level := &root
		for j, part := range parts {
			if part == "" {
				return nil, fmt.Errorf("fields: field %q: as: %q has an empty part", f.Name, f.As)
			}
			var n *keyNode
			for _, c := range *level {
				if c.key == part {
					n = c
					break
				}
			}
			last := j == len(parts)-1
			switch {
			case n == nil:
				n = &keyNode{key: part, field: -1}
				if last {
					n.field = i
				}
				*level = append(*level, n)
			case last || n.field >= 0:
				return nil, fmt.Errorf("fields: field %q: the key %q is written twice; choose another as", f.Name, path)
			}
			level = &n.children
		}
	}
	return root, nil
}

// Init checks the fields and passes the schema on: the records stay as they are.
func (t *output) Init(ic *transform.InitContext) (record.Schema, error) {
	for i, f := range t.Fields {
		ic.Use(f.Name, fmt.Sprintf("fields[%d].name", i))
	}
	return ic.In, nil
}

func (t *output) ndjson() bool {
	if t.Format == "" || t.Format == formatAuto {
		ext := strings.ToLower(filepath.Ext(t.Path))
		return ext == ".ndjson" || ext == ".jsonl"
	}
	return t.Format == formatNDJSON
}

func (t *output) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file := paths.Resolve(ctx, t.Path)
	info, err := os.Stat(file)
	switch {
	case err == nil && info.IsDir():
		return fmt.Errorf("cannot write %q: it is a folder", t.Path)
	case err == nil && t.IfExists == "fail":
		return fmt.Errorf("%q already exists (ifExists: fail)", t.Path)
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("cannot write %q: %w", t.Path, plain(err))
	}
	dir, base := filepath.Split(file)
	if dir == "" {
		dir = "."
	}
	f, err := os.CreateTemp(dir, "."+base+".*.tmp")
	if err != nil {
		return fmt.Errorf("cannot write %q: %w", t.Path, plain(err))
	}
	n, err := t.write(ctx, f, in, out)
	if cerr := f.Close(); err == nil && cerr != nil {
		err = fmt.Errorf("cannot write %q: %w", t.Path, plain(cerr))
	}
	if err == nil {
		if rerr := os.Rename(f.Name(), file); rerr != nil {
			err = fmt.Errorf("cannot write %q: %w", t.Path, plain(rerr))
		}
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	out.Logf(levelInfo, "wrote %d %s to %s", n, plural(n, "record"), t.Path)
	return nil
}

func (t *output) write(ctx context.Context, f *os.File, in <-chan transform.Batch, out *transform.Output) (int64, error) {
	w := bufio.NewWriterSize(f, 64*1024)
	nd := t.ndjson()
	var buf bytes.Buffer
	var n, seen int64
	for b := range in {
		for _, r := range b {
			seen++
			buf.Reset()
			if badField, err := t.object(&buf, r); err != nil {
				if err := out.Fail(r, badField, fmt.Errorf("%s: record %d, field %q: %w", t.Path, seen, badField, err)); err != nil {
					return n, err
				}
				continue
			}
			sep := "\n"
			if !nd {
				sep = map[bool]string{true: "[\n", false: ",\n"}[n == 0]
			}
			if n > 0 || !nd {
				if _, err := w.WriteString(sep); err != nil {
					return n, fmt.Errorf("cannot write %q: %w", t.Path, plain(err))
				}
			}
			if _, err := w.Write(buf.Bytes()); err != nil {
				return n, fmt.Errorf("cannot write %q: %w", t.Path, plain(err))
			}
			n++
			if err := out.Emit(r); err != nil {
				return n, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return n, err
	}
	end := "\n"
	switch {
	case !nd && n == 0:
		end = "[]\n"
	case !nd:
		end = "\n]\n"
	case n == 0:
		end = ""
	}
	if _, err := w.WriteString(end); err != nil {
		return n, fmt.Errorf("cannot write %q: %w", t.Path, plain(err))
	}
	if err := w.Flush(); err != nil {
		return n, fmt.Errorf("cannot write %q: %w", t.Path, plain(err))
	}
	return n, nil
}

// object writes a record as a JSON object, its keys in the order of the fields.
func (t *output) object(b *bytes.Buffer, r record.Record) (string, error) {
	b.WriteByte('{')
	write := func(i int, name string, v any, c *format.Codec) error {
		if i > 0 {
			b.WriteByte(',')
		}
		writeString(b, name)
		b.WriteByte(':')
		if c != nil && v != nil {
			s, err := c.Convert(v)
			if err != nil {
				return err
			}
			v = s
		}
		return writeValue(b, v)
	}
	if len(t.Fields) == 0 {
		for i, f := range r.Fields() {
			if err := write(i, f.Name, f.Value, nil); err != nil {
				return f.Name, err
			}
		}
	} else if name, err := t.keys(b, r, t.tree, write); err != nil {
		return name, err
	}
	b.WriteByte('}')
	return "", nil
}

// keys writes the keys of an object from the fields, nested objects in braces.
func (t *output) keys(b *bytes.Buffer, r record.Record, nodes []*keyNode, write func(int, string, any, *format.Codec) error) (string, error) {
	for i, n := range nodes {
		if n.field >= 0 {
			f := t.Fields[n.field]
			v, _ := r.Get(f.Name)
			if err := write(i, n.key, v, t.codecs[n.field]); err != nil {
				return f.Name, err
			}
			continue
		}
		if i > 0 {
			b.WriteByte(',')
		}
		writeString(b, n.key)
		b.WriteString(":{")
		if name, err := t.keys(b, r, n.children, write); err != nil {
			return name, err
		}
		b.WriteByte('}')
	}
	return "", nil
}

// writeValue writes a value in JSON: whole and exact numbers as numbers, dates as text
// in ISO 8601, lists and objects as they are.
func writeValue(b *bytes.Buffer, v any) error {
	switch v := v.(type) {
	case nil:
		b.WriteString("null")
	case string:
		writeString(b, v)
	case bool:
		b.WriteString(strconv.FormatBool(v))
	case int64:
		b.WriteString(strconv.FormatInt(v, 10))
	case decimal.Decimal:
		b.WriteString(v.String())
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("%v cannot be written in JSON", v)
		}
		b.WriteString(strconv.FormatFloat(v, 'f', -1, 64))
	case record.Date:
		writeString(b, v.String())
	case time.Time:
		writeString(b, v.Format(time.RFC3339Nano))
	default:
		data, err := stdjson.Marshal(v)
		if err != nil {
			return fmt.Errorf("the value cannot be written in JSON: %w", err)
		}
		b.Write(data)
	}
	return nil
}

// writeString writes text in quotes, escaped as JSON requires; < > & stay readable.
func writeString(b *bytes.Buffer, s string) {
	const hex = "0123456789abcdef"
	b.WriteByte('"')
	start := 0
	for i := range len(s) {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' {
			continue
		}
		b.WriteString(s[start:i])
		switch c {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteString(`\u00`)
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0xF])
		}
		start = i + 1
	}
	b.WriteString(s[start:])
	b.WriteByte('"')
}
