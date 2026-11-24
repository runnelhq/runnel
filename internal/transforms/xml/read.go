package xml

import (
	"bufio"
	"bytes"
	stdxml "encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"

	"github.com/runnelhq/runnel/internal/record"
)

// Limits for one record (an element with everything inside it): its size in the file
// and how deeply elements nest in it.
const (
	maxRecordSize = 16 << 20
	maxDepth      = 256
)

// textKey is the field of the text of an element that also has attributes or children.
const textKey = "#text"

// newDecoder returns a decoder for an XML file: a byte order mark of UTF-16 decides
// the encoding, else the declaration (<?xml … encoding="windows-1252"?>), else UTF-8.
func newDecoder(r io.Reader) (*stdxml.Decoder, error) {
	br := bufio.NewReader(r)
	head, _ := br.Peek(3)
	var text io.Reader = br
	converted := false
	switch {
	case bytes.HasPrefix(head, []byte{0xEF, 0xBB, 0xBF}):
		_, _ = br.Discard(3)
	case bytes.HasPrefix(head, []byte{0xFF, 0xFE}), bytes.HasPrefix(head, []byte{0xFE, 0xFF}):
		text = unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder().Reader(br)
		converted = true
	}
	d := stdxml.NewDecoder(text)
	d.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
		if converted {
			return input, nil // already UTF-8; the declaration still names UTF-16
		}
		switch strings.ToLower(label) {
		case "utf-8", "utf8", "us-ascii", "ascii":
			return input, nil
		case "windows-1252", "cp1252":
			return charmap.Windows1252.NewDecoder().Reader(input), nil
		case "iso-8859-1", "latin1", "latin-1":
			return charmap.ISO8859_1.NewDecoder().Reader(input), nil
		case "iso-8859-15", "latin9":
			return charmap.ISO8859_15.NewDecoder().Reader(input), nil
		case "utf-16", "utf-16le", "utf-16be":
			return nil, errors.New("the file declares UTF-16 but has no byte order mark")
		}
		return nil, fmt.Errorf("the encoding %q is not supported (UTF-8, UTF-16, Windows-1252, ISO-8859-1 and ISO-8859-15 are)", label)
	}
	return d, nil
}

// pattern is the path of the elements that are records: element names separated by /,
// matched against the end of the path of an element; a leading / anchors it at the root.
type pattern struct {
	names    []string
	anchored bool
}

func parsePattern(s string) (pattern, error) {
	p := pattern{anchored: strings.HasPrefix(s, "/")}
	s = strings.Trim(s, "/")
	if s == "" {
		return p, nil
	}
	for name := range strings.SplitSeq(s, "/") {
		if name == "" || strings.ContainsAny(name, "@[]*()= ") {
			return p, fmt.Errorf("records: %q is not a path of element names like orders/order", s)
		}
		p.names = append(p.names, localName(name))
	}
	return p, nil
}

// matches reports whether an element at this path (names from the root) is a record.
// Without names the records are the children of the root element.
func (p pattern) matches(path []string) bool {
	if len(p.names) == 0 {
		return len(path) == 2
	}
	if len(path) < len(p.names) || (p.anchored && len(path) != len(p.names)) {
		return false
	}
	tail := path[len(path)-len(p.names):]
	for i, n := range p.names {
		if tail[i] != n {
			return false
		}
	}
	return true
}

// localName drops a namespace prefix (ns:Order → Order): names are compared without
// namespaces.
func localName(s string) string {
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// element is an element of a record: its attributes, children and text.
type element struct {
	attrs    []stdxml.Attr
	children []*element
	names    []string // local names of the children, in order
	text     strings.Builder
}

// readElement reads the element whose start was just read, with everything inside it.
func readElement(d *stdxml.Decoder, start stdxml.StartElement, from int64) (*element, error) {
	e := &element{attrs: start.Attr}
	stack := []*element{e}
	for len(stack) > 0 {
		tok, err := d.Token()
		if err != nil {
			return nil, err
		}
		if d.InputOffset()-from > maxRecordSize {
			return nil, fmt.Errorf("a record (<%s>) is larger than %d MiB", start.Name.Local, maxRecordSize>>20)
		}
		top := stack[len(stack)-1]
		switch t := tok.(type) {
		case stdxml.StartElement:
			if len(stack) >= maxDepth {
				return nil, fmt.Errorf("elements in a record (<%s>) are nested more than %d deep", start.Name.Local, maxDepth)
			}
			child := &element{attrs: t.Attr}
			top.children = append(top.children, child)
			top.names = append(top.names, t.Name.Local)
			stack = append(stack, child)
		case stdxml.EndElement:
			stack = stack[:len(stack)-1]
		case stdxml.CharData:
			top.text.Write(t)
		}
	}
	return e, nil
}

// record turns the element of a record into fields: attributes as @name, children with
// text only as text, others as objects, repeated children as lists, the own text as #text.
func (e *element) record() record.Record {
	return record.FromFields(e.fields())
}

func (e *element) fields() []record.Field {
	var fields []record.Field
	for _, a := range e.attrs {
		if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
			continue // namespace declarations are no data
		}
		fields = append(fields, record.Field{Name: "@" + a.Name.Local, Value: a.Value})
	}
	index := map[string]int{} // position of a name in fields
	for i, c := range e.children {
		name := e.names[i]
		v := c.value()
		if j, ok := index[name]; ok {
			// Values are text or objects: a list comes only from repeated children.
			if list, ok := fields[j].Value.([]any); ok {
				list = append(list, v)
				fields[j].Value = list
			} else {
				fields[j].Value = []any{fields[j].Value, v}
			}
			continue
		}
		index[name] = len(fields)
		fields = append(fields, record.Field{Name: name, Value: v})
	}
	if text := strings.TrimSpace(e.text.String()); text != "" {
		fields = append(fields, record.Field{Name: textKey, Value: e.text.String()})
	}
	return fields
}

// value is the value of a child: its text, or an object with its attributes, children
// and text.
func (e *element) value() any {
	if len(e.attrs) == 0 && len(e.children) == 0 {
		return e.text.String()
	}
	fields := e.fields()
	m := make(map[string]any, len(fields))
	for _, f := range fields {
		m[f.Name] = f.Value
	}
	return m
}
