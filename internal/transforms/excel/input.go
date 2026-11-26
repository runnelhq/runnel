package excel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/xlsx"
)

// excel.input reads the rows of a sheet as records (S67a).
var inputDefinition = transform.Definition{
	Type:         "excel.input",
	RecordErrors: true,
	Category:     transform.CategoryInput,
	Label:        "Excel Input",
	Description:  "Reads the rows of a sheet of an Excel workbook (.xlsx) as records",
	Example:      "path: data/orders.xlsx\nsheet: Orders\nrange: A3\nfields:\n  - { name: Nr, type: int }\n  - { name: Date, type: date }\n  - { name: Amount, type: decimal }\n",
	Config:       input{},
	New:          newInput,
	DetectFields: detectFields,
	Reference: action.Reference{
		Outputs: []action.OutputField{{Name: "<fields[].name>", Type: "any", Description: "The cell with the type of the field; without fields, every column with the type of its cells (text, int, decimal, bool, date, datetime)"}},
		Records: "Creates one record per row of the table and passes it to all data links; it has no incoming links.",
		Errors:  "A row with an error value like #N/A in a cell, or a value that does not fit its type, goes to the error link with the cell in the message; without one, the pipeline stops. A missing sheet or column, .xls, .xlsb and workbooks with a password stop the pipeline.",
		Limits:  []string{"The sheet is read as a stream; its XML may unpack to at most 4 GB, the shared texts to 256 MB."},
	},
}

type input struct {
	Path      string  `json:"path" doc:"Workbook to read (.xlsx or .xlsm), relative to the project folder or absolute" schema:"required,example=data/orders.xlsx"`
	Sheet     string  `json:"sheet" doc:"Sheet to read: its name, or its number like 2; empty: the first sheet" schema:"example=Orders"`
	Range     string  `json:"range" doc:"Where the table is: its first cell like A3, with last column A3:F or with last row B2:D100; empty: from the first row with values" schema:"example=A3:F"`
	Header    *bool   `json:"header" doc:"Whether the first row of the table holds the names of the columns" schema:"default=true"`
	EmptyRows string  `json:"emptyRows" doc:"Empty rows in the table: skip them, or stop at the first one, e.g. when notes follow the table" schema:"enum=skip|stop,default=skip"`
	Fields    []field `json:"fields" doc:"Columns to read, with type and format, matched by name (or from) in the header or else by position; empty: all columns with the values of their cells"`

	codecs []format.Codec
}

// field is a column of the field definition (fields:).
type field struct {
	Name   string `json:"name" doc:"Name of the field" schema:"required,example=amount"`
	From   string `json:"from" doc:"Column in the header; empty: the column named like the field" schema:"example=Betrag"`
	Type   string `json:"type" doc:"Type of the value; empty: as in the cell (text, number, true/false, date)" schema:"enum=string|int|decimal|float|bool|date|datetime|json"`
	Format string `json:"format" doc:"For text in the cells: number pattern like #.##0,00 or date pattern like dd.MM.yyyy; numbers and dates of Excel need none" schema:"example=dd.MM.yyyy"`
}

func newInput(with map[string]any) (transform.Transform, error) {
	t := &input{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	if !hasVar(t.Range) {
		if _, err := parseRange(t.Range); err != nil {
			return nil, err
		}
	}
	if t.EmptyRows != "" && t.EmptyRows != "skip" && t.EmptyRows != "stop" {
		return nil, fmt.Errorf("invalid emptyRows %q (expected skip or stop)", t.EmptyRows)
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
		if f.From != "" && t.Header != nil && !*t.Header {
			return nil, fmt.Errorf("fields: field %q: from needs a header; without one the columns are read by position", f.Name)
		}
		var c format.Codec // without type: the value as in the cell
		if f.Type != "" {
			var err error
			if c, err = format.New(record.Type(f.Type), f.Format); err != nil {
				return nil, fmt.Errorf("field %q: %w", f.Name, err)
			}
		}
		t.codecs = append(t.codecs, c)
	}
	return t, nil
}

func hasVar(s string) bool { return strings.Contains(s, "${") }

// Init reports the schema: closed with fields (unknown type for fields without type),
// else open, as the columns are only known when reading.
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

func (t *input) Process(ctx context.Context, _ <-chan transform.Batch, out *transform.Output) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tb, err := openTable(ctx, t.Path, t.Sheet, t.Range, t.Header == nil || *t.Header, t.EmptyRows == "stop")
	if err != nil {
		return err
	}
	defer tb.close()
	cols, err := t.columns(tb)
	if err != nil {
		return err
	}
	var rows, failed int64
	values := make([]any, len(tb.names))
	for {
		n, cells, ok := tb.next()
		if !ok {
			break
		}
		rows++
		clear(values)
		var bad error
		var badField string
		for _, c := range cells {
			i := c.Col - tb.area.col
			if c.Error != "" {
				values[i] = c.Error
				if bad == nil && cols.reads(i) {
					bad, badField = fmt.Errorf("%s: the cell shows the error %s", tb.cell(c.Col, n), c.Error), cols.name(i)
				}
				continue
			}
			values[i] = c.Value
		}
		fields := make([]record.Field, 0, len(cols))
		for _, c := range cols {
			v := values[c.index]
			if bad == nil && c.typed && v != nil {
				var err error
				if v, err = t.convert(tb.book, c.codec, v); err != nil {
					bad, badField = fmt.Errorf("%s: field %q: %w", tb.cell(tb.area.col+c.index, n), c.name, err), c.name
				}
			}
			fields = append(fields, record.Field{Name: c.name, Value: v})
		}
		if bad != nil {
			// The row goes to an error link with the values of the cells, or stops the pipeline.
			failed++
			for i := range fields {
				fields[i].Value = values[cols[i].index]
			}
			if err := out.Fail(record.FromFields(fields), badField, bad); err != nil {
				return err
			}
			continue
		}
		if err := out.Emit(record.FromFields(fields)); err != nil {
			return err
		}
	}
	if err := tb.rows.Err(); err != nil {
		return fmt.Errorf("%s: %w", t.Path, err)
	}
	msg := fmt.Sprintf("read %d %s from %s", rows, plural(rows, "row"), tb.where)
	if failed > 0 {
		msg += fmt.Sprintf(", %d with errors", failed)
	}
	out.Logf(slog.LevelInfo, "%s", msg)
	return nil
}

// convert converts the value of a cell to the type of a field: text with the pattern,
// numbers to dates by the date system of Excel (a column whose dates show as numbers).
func (t *input) convert(b *xlsx.Book, c format.Codec, v any) (any, error) {
	if typ := c.Type(); typ == record.DateType || typ == record.DateTime {
		var n decimal.Decimal
		switch x := v.(type) {
		case int64:
			n = decimal.NewFromInt(x)
		case decimal.Decimal:
			n = x
		default:
			return c.Convert(v)
		}
		d, err := b.Date(n, typ == record.DateTime)
		if err != nil {
			return nil, err
		}
		return c.Convert(d)
	}
	return c.Convert(v)
}

// column is a column of the table read into a field.
type column struct {
	index int // position in the table, 0 for its first column
	name  string
	codec format.Codec
	typed bool
}

type columnList []column

func (l columnList) reads(i int) bool {
	for _, c := range l {
		if c.index == i {
			return true
		}
	}
	return false
}

func (l columnList) name(i int) string {
	for _, c := range l {
		if c.index == i {
			return c.name
		}
	}
	return ""
}

// columns decides which column goes into which field: without fields all columns of
// the table, else by name in the header or, without header, by position.
func (t *input) columns(tb *table) (columnList, error) {
	if len(t.Fields) == 0 {
		cols := make(columnList, len(tb.names))
		for i, name := range tb.names {
			cols[i] = column{index: i, name: name}
		}
		return cols, nil
	}
	cols := make(columnList, len(t.Fields))
	for i, f := range t.Fields {
		cols[i] = column{index: i, name: f.Name, codec: t.codecs[i], typed: f.Type != ""}
		if tb.header == 0 {
			continue
		}
		want := f.Name
		if f.From != "" {
			want = f.From
		}
		cols[i].index = -1
		for j, h := range tb.names {
			if h == want {
				cols[i].index = j
				break
			}
		}
		if cols[i].index < 0 {
			return nil, fmt.Errorf("%s, row %d: the header has no column %q%s (columns: %s)", tb.where, tb.header, want,
				model.Suggest(want, tb.names), strings.Join(tb.names, ", "))
		}
	}
	if tb.header == 0 && len(tb.names) < len(t.Fields) {
		// Without header the fields name the columns: also those beyond the first row.
		tb.names = append(tb.names, make([]string, len(t.Fields)-len(tb.names))...)
	}
	return cols, nil
}

func plural(n int64, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
