package json

import (
	"context"

	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// sampleValues is the number of values per field shown with a proposal.
const sampleValues = 5

// detectFields proposes the fields of json.input from its first records (S46): the keys
// of the objects in the order they appear, with the type of their values: numbers,
// true/false, lists and objects as in the file, text with a type and format proposed by
// format.Propose (dates, numbers written as text).
func detectFields(ctx context.Context, with map[string]any, limit int) (*transform.Detection, error) {
	n, err := newInput(with)
	if err != nil {
		return nil, err
	}
	t := n.(*input)
	t.Fields = nil // read the objects as they are
	d := &transform.Detection{Source: []transform.Fact{{Name: "File", Value: t.Path}, {Name: "Format", Value: t.mode()}}}
	if t.Records != "" {
		d.Source = append(d.Source, transform.Fact{Name: "Records", Value: t.Records})
	}
	var names []string
	values := map[string][]any{}
	var lines []int
	complete := true
	err = t.read(ctx, func(it item) (bool, error) {
		if d.Rows == limit {
			complete = false
			return false, nil
		}
		if it.err != nil {
			return false, it.err
		}
		d.Rows++
		lines = append(lines, it.line) // 0 in json: values have no line of their own
		for _, f := range it.r.Fields() {
			if _, ok := values[f.Name]; !ok {
				names = append(names, f.Name)
				values[f.Name] = make([]any, d.Rows-1) // empty in the records before
			}
		}
		for _, name := range names {
			v, _ := it.r.Get(name)
			values[name] = append(values[name], v)
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	d.Complete = complete
	for _, name := range names {
		d.Fields = append(d.Fields, propose(name, values[name], lines))
	}
	if err := transform.MergeFields(d, with["fields"]); err != nil {
		return nil, err
	}
	return d, nil
}

// propose proposes the type of a key from its values.
func propose(name string, values []any, lines []int) transform.DetectedField {
	f := transform.DetectedField{Name: name}
	kinds := map[string]bool{}
	var texts []string
	var textLines []int
	for i, v := range values {
		if len(f.Values) < sampleValues {
			f.Values = append(f.Values, expr.Format(v))
		}
		switch v := v.(type) {
		case nil:
		case string:
			kinds["text"] = true
			texts = append(texts, v)
			if lines[i] > 0 {
				textLines = append(textLines, lines[i])
			}
		case int64:
			kinds["int"] = true
		case bool:
			kinds["bool"] = true
		case map[string]any, []any:
			kinds["json"] = true
		default:
			kinds["decimal"] = true
		}
	}
	switch {
	case len(kinds) == 0:
		f.Type = string(record.String)
		f.Note = "only empty values; kept as text"
	case len(kinds) == 1 && kinds["text"]:
		if len(textLines) != len(texts) {
			textLines = nil
		}
		p := format.Propose(texts, textLines)
		f.Type, f.Format, f.Note, f.Warning = string(p.Type), p.Format, p.Note, p.Warning
	case len(kinds) == 1 && kinds["int"]:
		f.Type = string(record.Int)
	case len(kinds) <= 2 && (kinds["int"] || kinds["decimal"]) && !kinds["text"] && !kinds["bool"] && !kinds["json"]:
		f.Type = string(record.Decimal)
	case len(kinds) == 1 && kinds["bool"]:
		f.Type = string(record.Bool)
	case len(kinds) == 1 && kinds["json"]:
		f.Type = string(record.JSON)
		f.Note = "lists or objects; read single values with from: and a path like address.city"
	default:
		f.Type = string(record.String)
		f.Note = "values of different kinds; kept as text"
		f.Warning = true
	}
	return f
}
