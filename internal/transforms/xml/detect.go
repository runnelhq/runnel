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

// sampleValues is the number of values per field shown with a proposal.
const sampleValues = 5

// detectFields proposes the fields of xml.input from its first records (S46): the
// attributes and child elements in the order they appear, with a type from their text
// (format.Propose); elements with children or repeated ones as json. Without records
// it names the most repeated element, in case the records are deeper in the file.
func detectFields(ctx context.Context, with map[string]any, limit int) (*transform.Detection, error) {
	n, err := newInput(with)
	if err != nil {
		return nil, err
	}
	t := n.(*input)
	where := "the children of the root element"
	if t.Records != "" {
		where = "<" + t.Records + ">"
	}
	d := &transform.Detection{Source: []transform.Fact{{Name: "File", Value: t.Path}, {Name: "Records", Value: where}}}
	var names []string
	values := map[string][]any{}
	d.Complete = true
	err = t.read(ctx, func(r record.Record, _ int) (bool, error) {
		if d.Rows == limit {
			d.Complete = false
			return false, nil
		}
		d.Rows++
		for _, f := range r.Fields() {
			if _, ok := values[f.Name]; !ok {
				names = append(names, f.Name)
				values[f.Name] = make([]any, d.Rows-1) // empty in the records before
			}
		}
		for _, name := range names {
			v, _ := r.Get(name)
			values[name] = append(values[name], v)
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		d.Fields = append(d.Fields, propose(name, values[name]))
	}
	if t.Records == "" {
		if hint := repeated(ctx, t.Path, limit); hint != "" {
			d.Source = append(d.Source, transform.Fact{Name: "Hint", Value: hint})
		}
	}
	if err := transform.MergeFields(d, with["fields"]); err != nil {
		return nil, err
	}
	return d, nil
}

// propose proposes the type of a field from its values: text by format.Propose, an
// element with attributes by its text, elements with children or repeated ones as json.
func propose(name string, values []any) transform.DetectedField {
	f := transform.DetectedField{Name: name}
	var texts []string
	nested, lists := false, false
	for _, v := range values {
		switch x := v.(type) {
		case string:
			texts = append(texts, x)
		case map[string]any:
			if text, ok := x[textKey].(string); ok && onlyAttributes(x) {
				texts = append(texts, text)
				v = text
			} else {
				nested = true
			}
		case []any:
			lists = true
		}
		if len(f.Values) < sampleValues {
			f.Values = append(f.Values, sample(v))
		}
	}
	switch {
	case lists:
		f.Type, f.Note = string(record.JSON), "repeated element: a list; read single values with their own fields"
	case nested:
		f.Type, f.Note = string(record.JSON), "element with child elements; read single values with from: "+name+"/child"
	case len(texts) == 0:
		f.Type, f.Note = string(record.String), "No values: kept as text"
	default:
		p := format.Propose(texts, nil)
		f.Type, f.Format, f.Note, f.Warning = string(p.Type), p.Format, p.Note, p.Warning
	}
	return f
}

// onlyAttributes reports an element with text and attributes but no children.
func onlyAttributes(m map[string]any) bool {
	for k := range m {
		if k != textKey && !strings.HasPrefix(k, "@") {
			return false
		}
	}
	return true
}

func sample(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []any:
		return fmt.Sprintf("(%d elements)", len(x))
	}
	return "(element)"
}

// repeated names the element that repeats most often in the first elements of the
// file, when it is not a child of the root: a hint for records.
func repeated(ctx context.Context, path string, limit int) string {
	f, err := os.Open(paths.Resolve(ctx, path))
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	d, err := newDecoder(f)
	if err != nil {
		return ""
	}
	counts := map[string]int{}
	var order []string
	var stack []string
	for seen := 0; seen < limit*20; {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) || err != nil {
			break
		}
		switch e := tok.(type) {
		case stdxml.StartElement:
			stack = append(stack, e.Name.Local)
			key := strings.Join(stack, "/")
			if counts[key] == 0 {
				order = append(order, key)
			}
			counts[key]++
			seen++
		case stdxml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
	best := ""
	for _, k := range order {
		if counts[k] > counts[best] {
			best = k
		}
	}
	if best == "" || counts[best] < 2 || strings.Count(best, "/") == 1 {
		return "" // the children of the root repeat most: the default fits
	}
	return fmt.Sprintf("<%s> appears %d times; to read it as records set records: %s", best[strings.LastIndexByte(best, '/')+1:], counts[best], best)
}
