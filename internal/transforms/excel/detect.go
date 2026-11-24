package excel

import (
	"context"
	"strconv"
	"time"

	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/xlsx"
)

// sampleValues is the number of values per field shown with a proposal.
const sampleValues = 5

// detectFields proposes the fields of excel.input from the first rows of its table
// (S46): the names from the header (else field1, field2 …) and a type per column from
// its cells: numbers, dates and true/false as Excel stores them, text with a type and
// format proposed by format.Propose (e.g. dates typed as text).
func detectFields(ctx context.Context, with map[string]any, limit int) (*transform.Detection, error) {
	n, err := newInput(with)
	if err != nil {
		return nil, err
	}
	t := n.(*input)
	header := t.Header == nil || *t.Header
	tb, err := openTable(ctx, t.Path, t.Sheet, t.Range, header, t.EmptyRows == "stop")
	if err != nil {
		return nil, err
	}
	defer tb.close()
	d := &transform.Detection{ByPosition: !header, Source: []transform.Fact{
		{Name: "File", Value: t.Path},
		{Name: "Sheet", Value: tb.sheet},
		{Name: "Header", Value: map[bool]string{true: "row " + strconv.Itoa(tb.header), false: "none"}[header && tb.header > 0]},
	}}
	if tb.names != nil {
		d.Source = append(d.Source, transform.Fact{Name: "Columns", Value: xlsx.ColumnName(tb.area.col) + " to " + xlsx.ColumnName(tb.area.col+len(tb.names)-1)})
	}
	values := make([][]any, len(tb.names))
	errs := make([]int, len(tb.names))
	d.Complete = true
	for {
		_, cells, ok := tb.next()
		if !ok {
			break
		}
		if d.Rows == limit {
			d.Complete = false
			break
		}
		d.Rows++
		for i := range values {
			values[i] = append(values[i], nil)
		}
		for _, c := range cells {
			i := c.Col - tb.area.col
			if c.Error != "" {
				errs[i]++ // not a value of the column: the row would go to the error link
				continue
			}
			values[i][d.Rows-1] = c.Value
		}
	}
	if err := tb.rows.Err(); err != nil {
		return nil, err
	}
	for i, name := range tb.names {
		f := propose(name, values[i])
		if errs[i] > 0 {
			f.Warning = true
			f.Note = strconv.Itoa(errs[i]) + " of the cells show an error like #N/A; their rows go to the error link"
			if errs[i] == 1 {
				f.Note = "1 cell shows an error like #N/A; its row goes to the error link"
			}
		}
		d.Fields = append(d.Fields, f)
	}
	if err := transform.MergeFields(d, with["fields"]); err != nil {
		return nil, err
	}
	return d, nil
}

// text writes the value of a cell as text, e.g. for a sample or a column name: dates in
// ISO 8601, without quotes.
func text(v any) string {
	switch v := v.(type) {
	case record.Date:
		return v.String()
	case time.Time:
		return v.Format("2006-01-02 15:04:05")
	}
	return expr.Format(v)
}

// propose proposes the type of a column from the values of its cells.
func propose(name string, values []any) transform.DetectedField {
	f := transform.DetectedField{Name: name}
	kinds := map[record.Type]bool{}
	var texts []string
	for _, v := range values {
		if len(f.Values) < sampleValues {
			f.Values = append(f.Values, text(v))
		}
		switch v := v.(type) {
		case nil:
		case string:
			kinds[record.String] = true
			texts = append(texts, v)
		case int64:
			kinds[record.Int] = true
		case bool:
			kinds[record.Bool] = true
		case record.Date:
			kinds[record.DateType] = true
		case time.Time:
			kinds[record.DateTime] = true
		default:
			kinds[record.Decimal] = true
		}
	}
	switch {
	case len(kinds) == 0:
		f.Type, f.Note = string(record.String), "only empty cells; kept as text"
	case len(kinds) == 1 && kinds[record.String]:
		p := format.Propose(texts, nil)
		f.Type, f.Format, f.Note, f.Warning = string(p.Type), p.Format, p.Note, p.Warning
	case len(kinds) == 1:
		for k := range kinds {
			f.Type = string(k)
		}
	case len(kinds) == 2 && kinds[record.Int] && kinds[record.Decimal]:
		f.Type = string(record.Decimal)
	case len(kinds) == 2 && kinds[record.DateType] && kinds[record.DateTime]:
		f.Type = string(record.DateTime)
	default:
		f.Type, f.Warning = string(record.String), true
		f.Note = "cells of different kinds (e.g. numbers and text); kept as text"
	}
	return f
}
