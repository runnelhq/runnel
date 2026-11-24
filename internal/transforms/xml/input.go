package xml

import (
	"context"
	stdxml "encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// xml.input reads the elements of an XML file that are records (S68a).
var inputDefinition = transform.Definition{
	Type:         "xml.input",
	RecordErrors: true,
	Category:     transform.CategoryInput,
	Label:        "XML Input",
	Description:  "Reads the elements of an XML file as records",
	Example:      "path: data/orders.xml\nrecords: orders/order\nfields:\n  - { name: id, from: \"@id\", type: int }\n  - { name: customer, from: customer/name }\n  - { name: amount, type: decimal }\n",
	Config:       input{},
	New:          newInput,
	DetectFields: detectFields,
}

type input struct {
	Path    string  `json:"path" doc:"XML file to read, relative to the project folder or absolute; the encoding comes from the byte order mark or the declaration" schema:"required,example=data/orders.xml"`
	Records string  `json:"records" doc:"Elements that are records: a path of element names with /, matched at the end of the path of an element (a leading / starts at the root); empty: the children of the root element" schema:"example=orders/order"`
	Fields  []field `json:"fields" doc:"Fields to read, with path and type; empty: attributes as @name and child elements as they are (text, objects, lists)"`

	pattern pattern
	codecs  []format.Codec
	from    [][]string // path of each field
}

// field is a field of the definition (fields:).
type field struct {
	Name   string `json:"name" doc:"Name of the field" schema:"required,example=amount"`
	From   string `json:"from" doc:"Where the value is in the record element: a child like amount, a path like customer/name, an attribute like @id or customer/@id, or #text for its own text; empty: the child named like the field" schema:"example=customer/name"`
	Type   string `json:"type" doc:"Type of the value; empty: as read (text, or objects and lists for elements with children)" schema:"enum=string|int|decimal|float|bool|date|datetime|json"`
	Format string `json:"format" doc:"Number pattern like #.##0,00 or date pattern like dd.MM.yyyy for the text; empty: 1234.5 and ISO dates" schema:"example=dd.MM.yyyy"`
}

func newInput(with map[string]any) (transform.Transform, error) {
	t := &input{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	var err error
	if t.pattern, err = parsePattern(t.Records); err != nil {
		return nil, err
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
		var c format.Codec // without type: the value as read
		if f.Type != "" {
			if c, err = format.New(record.Type(f.Type), f.Format); err != nil {
				return nil, fmt.Errorf("field %q: %w", f.Name, err)
			}
		}
		t.codecs = append(t.codecs, c)
		from := f.From
		if from == "" {
			from = f.Name
		}
		steps, err := parseFrom(from)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", f.Name, err)
		}
		t.from = append(t.from, steps)
	}
	return t, nil
}

// parseFrom reads the path of a field: names of elements, an attribute (@id) or #text
// only at the end.
func parseFrom(s string) ([]string, error) {
	steps := strings.Split(strings.Trim(s, "/"), "/")
	for i, step := range steps {
		last := i == len(steps)-1
		switch {
		case step == "", strings.ContainsAny(step, "[]*()= "):
			return nil, fmt.Errorf("from: %q is not a path like customer/name or @id", s)
		case (strings.HasPrefix(step, "@") || step == textKey) && !last:
			return nil, fmt.Errorf("from: %q: an attribute or #text can only be the last part", s)
		case strings.HasPrefix(step, "@"):
			steps[i] = "@" + localName(step[1:])
		default:
			steps[i] = localName(step)
		}
	}
	return steps, nil
}

// Init reports the schema: with fields closed, with their types; without them open.
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

// read reads the record elements of the file one after another and calls each with the
// record and the line it starts on; each returning false ends reading.
func (t *input) read(ctx context.Context, each func(r record.Record, line int) (bool, error)) error {
	f, err := os.Open(paths.Resolve(ctx, t.Path))
	if err != nil {
		return fmt.Errorf("cannot read %q: %w", t.Path, plain(err))
	}
	defer func() { _ = f.Close() }()
	d, err := newDecoder(f)
	if err != nil {
		return err
	}
	var path []string
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		offset := d.InputOffset()
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return nil // also an empty file: no records
		}
		if err != nil {
			return t.syntaxError(d, err)
		}
		switch e := tok.(type) {
		case stdxml.StartElement:
			path = append(path, e.Name.Local)
			if !t.pattern.matches(path) {
				continue
			}
			line, _ := d.InputPos()
			el, err := readElement(d, e, offset)
			if err != nil {
				return t.syntaxError(d, err)
			}
			path = path[:len(path)-1]
			more, err := each(el.record(), line)
			if err != nil || !more {
				return err
			}
		case stdxml.EndElement:
			path = path[:len(path)-1]
		}
	}
}

// syntaxError places an error of the XML in the file.
func (t *input) syntaxError(d *stdxml.Decoder, err error) error {
	if se, ok := errors.AsType[*stdxml.SyntaxError](err); ok {
		return fmt.Errorf("%s:%d: XML error: %s", t.Path, se.Line, se.Msg)
	}
	line, col := d.InputPos()
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%s:%d:%d: XML error: the file ends inside an element", t.Path, line, col)
	}
	return fmt.Errorf("%s:%d:%d: %w", t.Path, line, col, err)
}

func (t *input) Process(ctx context.Context, _ <-chan transform.Batch, out *transform.Output) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var n, failed int64
	err := t.read(ctx, func(r record.Record, line int) (bool, error) {
		n++
		sel, badField, err := t.selectFields(r)
		if err != nil {
			failed++
			return true, out.Fail(r, badField, fmt.Errorf("%s:%d: record %d, field %q: %w", t.Path, line, n, badField, err))
		}
		return true, out.Emit(sel)
	})
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("read %d %s from %s", n, plural(n, "record"), t.Path)
	if failed > 0 {
		msg += fmt.Sprintf(", %d with errors", failed)
	}
	out.Logf(levelInfo, "%s", msg)
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
		v, err := lookup(r, t.from[i])
		if err == nil && f.Type != "" && f.Type != string(record.JSON) && v != nil {
			v, err = scalar(v)
			if err == nil && v != nil {
				v, err = t.codecs[i].Convert(v)
			}
		}
		if err != nil {
			return record.Record{}, f.Name, err
		}
		fields[i] = record.Field{Name: f.Name, Value: v}
	}
	return record.FromFields(fields), "", nil
}

// scalar returns the text of a value for a field with a type: an element with
// attributes (<amount currency="EUR">12.50</amount>) gives its text.
func scalar(v any) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		if text, ok := x[textKey]; ok {
			return text, nil
		}
		return nil, errors.New("the element has child elements, not a single value; read it with type json or choose a child with from")
	case []any:
		return nil, fmt.Errorf("the element appears %d times; read it as a list with type json", len(x))
	}
	return v, nil
}

// lookup follows the path of a field in a record: nil when a part is missing; an error
// when a part before the last appears more than once.
func lookup(r record.Record, steps []string) (any, error) {
	v, ok := r.Get(steps[0])
	if !ok {
		return nil, nil
	}
	for i, step := range steps[1:] {
		switch x := v.(type) {
		case map[string]any:
			v = x[step]
		case []any:
			return nil, fmt.Errorf("<%s> appears %d times; read it as a list with type json", strings.Join(steps[:i+1], "/"), len(x))
		default:
			return nil, nil // text has no children
		}
	}
	return v, nil
}
