package xml

import (
	"bufio"
	"bytes"
	"context"
	stdxml "encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// xml.output writes records as elements of an XML file and passes them on (S68b). A
// new file replaces the old one only once all records are written.
var outputDefinition = transform.Definition{
	Type:         "xml.output",
	RecordErrors: true,
	Category:     transform.CategoryOutput,
	Label:        "XML Output",
	Description:  "Writes records as elements of an XML file and passes them on",
	Example:      "path: out/orders.xml\nroot: orders\nrecord: order\nfields:\n  - { name: id, as: \"@id\" }\n  - { name: customer, as: customer/name }\n  - { name: amount }\n",
	Config:       output{},
	New:          newOutput,
	Reference: action.Reference{
		Records: "Passes every written record on unchanged to all data links.",
		Errors:  "A record with an invalid element or attribute name, a control character or a doubled attribute goes to the error link and is not written; without one, the pipeline stops. With ifExists: fail, an existing file stops the pipeline. The file is written to a temporary file first and replaces an existing one only once all records are written, so a failure leaves the old file.",
	},
}

type output struct {
	Path     string     `json:"path" doc:"XML file to write (UTF-8), relative to the project folder or absolute" schema:"required,example=out/orders.xml"`
	Root     string     `json:"root" doc:"Name of the root element; empty: records" schema:"example=orders"`
	Record   string     `json:"record" doc:"Name of the element of each record; empty: record" schema:"example=order"`
	IfExists string     `json:"ifExists" doc:"If the file exists: replace it once all records are written, or fail" schema:"enum=replace|fail,default=replace"`
	Nulls    string     `json:"nulls" doc:"Empty values: skip writes no element, empty writes an empty one" schema:"enum=skip|empty,default=skip"`
	Fields   []outField `json:"fields" doc:"Fields to write, in this order, with where they go; empty: all fields of the records as child elements"`

	codecs  []*format.Codec
	targets [][]string
}

type outField struct {
	Name   string `json:"name" doc:"Name of the field to write" schema:"required,format=runnel-field,example=amount"`
	As     string `json:"as" doc:"Where the value goes in the record element: a child like amount, a path like customer/name, an attribute like @id or customer/@id, or #text; empty: a child named like the field" schema:"example=customer/name"`
	Format string `json:"format" doc:"Writes the value with a number pattern like #.##0,00 or a date pattern like dd.MM.yyyy; empty: numbers like 1234.5, dates in ISO 8601" schema:"example=dd.MM.yyyy"`
}

func newOutput(with map[string]any) (transform.Transform, error) {
	t := &output{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	switch t.IfExists {
	case "", "replace", "fail":
	default:
		return nil, fmt.Errorf("invalid ifExists %q (expected replace or fail)", t.IfExists)
	}
	switch t.Nulls {
	case "", "skip", "empty":
	default:
		return nil, fmt.Errorf("invalid nulls %q (expected skip or empty)", t.Nulls)
	}
	for field, name := range map[string]string{"root": t.Root, "record": t.Record} {
		if name != "" && !hasVar(name) {
			if err := checkName(name); err != nil {
				return nil, fmt.Errorf("%s: %w", field, err)
			}
		}
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
		as := f.As
		if as == "" {
			as = f.Name
		}
		steps, err := parseTarget(as)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", f.Name, err)
		}
		t.targets = append(t.targets, steps)
	}
	return t, nil
}

func hasVar(s string) bool { return strings.Contains(s, "${") }

// checkName checks the name of an element or attribute: a letter or _ first, then
// letters, digits, _ - and .; no namespace prefix.
func checkName(name string) error {
	if strings.Contains(name, ":") {
		return fmt.Errorf("%q has a namespace prefix, which xml.output does not write yet", name)
	}
	for i, r := range name {
		start := unicode.IsLetter(r) || r == '_'
		rest := i > 0 && (unicode.IsDigit(r) || r == '-' || r == '.')
		if !start && !rest {
			return fmt.Errorf("%q is not a valid XML name (letters, digits, _ - and ., starting with a letter or _); set fields: with as", name)
		}
	}
	if name == "" || strings.HasPrefix(strings.ToLower(name), "xml") {
		return fmt.Errorf("%q is not a valid XML name", name)
	}
	return nil
}

// parseTarget reads where a field goes: names of elements, an attribute (@id) or #text
// only at the end.
func parseTarget(s string) ([]string, error) {
	steps := strings.Split(strings.Trim(s, "/"), "/")
	for i, step := range steps {
		last := i == len(steps)-1
		name := strings.TrimPrefix(step, "@")
		switch {
		case (strings.HasPrefix(step, "@") || step == textKey) && !last:
			return nil, fmt.Errorf("as: %q: an attribute or #text can only be the last part", s)
		case step == textKey:
		default:
			if err := checkName(name); err != nil {
				return nil, fmt.Errorf("as: %w", err)
			}
		}
	}
	return steps, nil
}

// Init checks the fields and passes the schema on: the records stay as they are.
func (t *output) Init(ic *transform.InitContext) (record.Schema, error) {
	for i, f := range t.Fields {
		ic.Use(f.Name, fmt.Sprintf("fields[%d].name", i))
	}
	return ic.In, nil
}

func (t *output) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, rec := orDefault(t.Root, "records"), orDefault(t.Record, "record")
	for _, name := range []string{root, rec} {
		if err := checkName(name); err != nil {
			return err
		}
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
	n, err := t.write(ctx, f, root, rec, in, out)
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

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func (t *output) write(ctx context.Context, f *os.File, root, rec string, in <-chan transform.Batch, out *transform.Output) (int64, error) {
	w := bufio.NewWriterSize(f, 64*1024)
	writeErr := func(err error) error { return fmt.Errorf("cannot write %q: %w", t.Path, plain(err)) }
	if _, err := w.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n<" + root + ">\n"); err != nil {
		return 0, writeErr(err)
	}
	var buf bytes.Buffer
	var n, seen int64
	for b := range in {
		for _, r := range b {
			seen++
			e, badField, err := t.element(rec, r)
			if err == nil {
				buf.Reset()
				err = e.write(&buf, 1)
			}
			if err != nil {
				if err := out.Fail(r, badField, fmt.Errorf("%s: record %d, field %q: %w", t.Path, seen, badField, err)); err != nil {
					return n, err
				}
				continue
			}
			if _, err := w.Write(buf.Bytes()); err != nil {
				return n, writeErr(err)
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
	if _, err := w.WriteString("</" + root + ">\n"); err != nil {
		return n, writeErr(err)
	}
	if err := w.Flush(); err != nil {
		return n, writeErr(err)
	}
	return n, nil
}

// node is an element to write.
type node struct {
	name     string
	attrs    [][2]string
	text     string
	hasText  bool
	children []*node
}

// element builds the element of a record from its fields.
func (t *output) element(name string, r record.Record) (*node, string, error) {
	e := &node{name: name}
	empty := t.Nulls == "empty"
	if len(t.Fields) == 0 {
		for _, f := range r.Fields() {
			if err := checkName(f.Name); err != nil {
				return nil, f.Name, err
			}
			if err := e.set([]string{f.Name}, f.Value, empty); err != nil {
				return nil, f.Name, err
			}
		}
		return e, "", nil
	}
	for i, f := range t.Fields {
		v, _ := r.Get(f.Name)
		if c := t.codecs[i]; c != nil && v != nil {
			s, err := c.Convert(v)
			if err != nil {
				return nil, f.Name, err
			}
			v = s
		}
		if err := e.set(t.targets[i], v, empty); err != nil {
			return nil, f.Name, err
		}
	}
	return e, "", nil
}

// set puts a value at a path below the element: child elements (created once, so that
// customer/name and customer/@id share one customer), an attribute or the text.
func (e *node) set(steps []string, v any, empty bool) error {
	if v == nil && !empty {
		return nil
	}
	parent := e
	for _, step := range steps[:len(steps)-1] {
		var next *node
		for _, c := range parent.children {
			if c.name == step {
				next = c
			}
		}
		if next == nil {
			next = &node{name: step}
			parent.children = append(parent.children, next)
		}
		parent = next
	}
	last := steps[len(steps)-1]
	switch {
	case strings.HasPrefix(last, "@"):
		s, err := scalarText(v)
		if err != nil {
			return err
		}
		for _, a := range parent.attrs {
			if a[0] == last[1:] {
				return fmt.Errorf("the attribute %s of <%s> is written twice", last, parent.name)
			}
		}
		parent.attrs = append(parent.attrs, [2]string{last[1:], s})
		return nil
	case last == textKey:
		s, err := scalarText(v)
		if err != nil {
			return err
		}
		parent.text, parent.hasText = s, true
		return nil
	}
	if list, ok := v.([]any); ok {
		for _, item := range list {
			if err := parent.add(last, item); err != nil {
				return err
			}
		}
		return nil
	}
	return parent.add(last, v)
}

// add adds a child with a value: text, or for an object its attributes (@x), text
// (#text) and children, its keys sorted.
func (e *node) add(name string, v any) error {
	c := &node{name: name}
	e.children = append(e.children, c)
	m, ok := v.(map[string]any)
	if !ok {
		if v == nil {
			return nil
		}
		s, err := scalarText(v)
		c.text, c.hasText = s, true
		return err
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		name := strings.TrimPrefix(k, "@")
		if k != textKey {
			if err := checkName(name); err != nil {
				return err
			}
		}
		if err := c.set([]string{k}, m[k], false); err != nil {
			return err
		}
	}
	return nil
}

// scalarText writes a single value as text: numbers exactly, dates in ISO 8601.
func scalarText(v any) (string, error) {
	var s string
	switch x := v.(type) {
	case nil:
	case string:
		s = x
	case bool:
		s = strconv.FormatBool(x)
	case int64:
		s = strconv.FormatInt(x, 10)
	case decimal.Decimal:
		s = x.String()
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return "", fmt.Errorf("%v cannot be written in XML", x)
		}
		s = strconv.FormatFloat(x, 'f', -1, 64)
	case record.Date:
		s = x.String()
	case time.Time:
		s = x.Format(time.RFC3339Nano)
	case map[string]any, []any:
		return "", errors.New("a list or object cannot be an attribute or text; write it as an element")
	default:
		s = fmt.Sprint(x)
	}
	for _, r := range s {
		if r == utf8.RuneError || (r < 0x20 && r != '\t' && r != '\n' && r != '\r') {
			return "", fmt.Errorf("the text contains the character %U, which XML cannot hold", r)
		}
	}
	return s, nil
}

// write writes the element indented by depth, children on lines of their own.
func (e *node) write(b *bytes.Buffer, depth int) error {
	indent := strings.Repeat("  ", depth)
	b.WriteString(indent)
	b.WriteByte('<')
	b.WriteString(e.name)
	for _, a := range e.attrs {
		b.WriteByte(' ')
		b.WriteString(a[0])
		b.WriteString(`="`)
		if err := stdxml.EscapeText(b, []byte(a[1])); err != nil {
			return err
		}
		b.WriteByte('"')
	}
	if !e.hasText && len(e.children) == 0 {
		b.WriteString("/>\n")
		return nil
	}
	b.WriteByte('>')
	if e.hasText {
		if err := stdxml.EscapeText(b, []byte(e.text)); err != nil {
			return err
		}
	}
	if len(e.children) > 0 {
		b.WriteByte('\n')
		for _, c := range e.children {
			if err := c.write(b, depth+1); err != nil {
				return err
			}
		}
		b.WriteString(indent)
	}
	b.WriteString("</")
	b.WriteString(e.name)
	b.WriteString(">\n")
	return nil
}
