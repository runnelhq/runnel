package csv

import (
	"context"
	stdcsv "encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// csv.input reads the rows of a CSV file as records.
var inputDefinition = transform.Definition{
	Type:         "csv.input",
	RecordErrors: true,
	Category:     transform.CategoryInput,
	Label:        "CSV Input",
	Description:  "Reads the rows of a CSV file as records",
	Config:       input{},
	New:          newInput,
}

type input struct {
	Path      string `json:"path" doc:"CSV file to read; relative paths refer to the working directory of runnel" schema:"required,example=C:\\in\\orders.csv"`
	Delimiter string `json:"delimiter" doc:"Character between the values, e.g. ; or \\t for tab; empty: comma" schema:"example=;"`
	Header    *bool  `json:"header" doc:"Whether the first row holds the names of the columns" schema:"default=true"`
	Encoding  string `json:"encoding" doc:"Encoding of the file; auto recognizes UTF-8 and UTF-16 by their byte order mark and reads UTF-8 otherwise" schema:"enum=auto|utf-8|windows-1252|utf-16,default=auto"`
	// Fields selects and converts columns; without it all columns are text.
	Fields []field `json:"fields" doc:"Columns to read, with type and format, matched by name in the header or else by position; empty: all columns as text"`

	codecs []codec
}

func newInput(with map[string]any) (transform.Transform, error) {
	t := &input{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	codecs, err := codecsOf(t.Fields)
	if err != nil {
		return nil, err
	}
	t.codecs = codecs
	if !hasVar(t.Delimiter) {
		if _, err := delimiter(t.Delimiter); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// codecsOf checks the field definition and returns a codec per field.
func codecsOf(fields []field) ([]codec, error) {
	codecs := make([]codec, len(fields))
	seen := map[string]bool{}
	for i, f := range fields {
		if f.Name == "" {
			return nil, errors.New("fields: every field needs a name")
		}
		if seen[f.Name] {
			return nil, fmt.Errorf("fields: the field %q is listed twice", f.Name)
		}
		seen[f.Name] = true
		c, err := newCodec(f)
		if err != nil {
			return nil, err
		}
		codecs[i] = c
	}
	return codecs, nil
}

// hasVar reports whether a value still contains a variable (runnel validate checks the
// configuration before variables are resolved).
func hasVar(s string) bool { return strings.Contains(s, "${") }

// delimiter returns the single character between values; empty is a comma.
func delimiter(s string) (rune, error) {
	switch s {
	case "":
		return ',', nil
	case `\t`, "tab":
		return '\t', nil
	}
	r, size := utf8.DecodeRuneInString(s)
	if size != len(s) || r == '"' || r == '\r' || r == '\n' || r == utf8.RuneError {
		return 0, fmt.Errorf("delimiter: %q is not a single character like , or ; (\\t for tab)", s)
	}
	return r, nil
}

// Init reports the schema of the records: with a field definition closed, with the
// types of the fields; without it open, as the columns are only known when reading.
func (t *input) Init(*transform.InitContext) (record.Schema, error) {
	if len(t.Fields) == 0 {
		return record.Schema{Open: true}, nil
	}
	var s record.Schema
	for i, f := range t.Fields {
		s = s.With(f.Name, t.codecs[i].Type())
	}
	return s, nil
}

func (t *input) Process(ctx context.Context, _ <-chan transform.Batch, out *transform.Output) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	comma, err := delimiter(t.Delimiter)
	if err != nil {
		return err
	}
	f, err := os.Open(t.Path)
	if err != nil {
		return fmt.Errorf("cannot read %q: %w", t.Path, plain(err))
	}
	defer func() { _ = f.Close() }()
	text, err := decoder(f, t.Encoding)
	if err != nil {
		return err
	}
	r := stdcsv.NewReader(text)
	r.Comma = comma
	r.ReuseRecord = true

	header := t.Header == nil || *t.Header
	var cols []column // which value goes into which field
	var rows, failed int64
	for {
		row, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if errors.Is(err, stdcsv.ErrFieldCount) && cols != nil {
			// A row with more or fewer values can go to an error link, as text.
			rows++
			failed++
			if err := out.Fail(raw(cols, row), "", t.readError(err)); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return t.readError(err)
		}
		if cols == nil {
			if cols, err = t.columns(row, header); err != nil {
				return err
			}
			if header {
				continue
			}
		}
		rows++
		// The names of the columns are unique (checked by columns): the record is built at once.
		fields := make([]record.Field, 0, len(cols))
		var bad error
		var badField string
		for _, c := range cols {
			v, err := c.codec.Parse(value(row, c.index))
			if err != nil {
				line, col := r.FieldPos(c.index)
				bad, badField = fmt.Errorf("%s:%d:%d: field %q: %w", t.Path, line, col, c.name, err), c.name
				break
			}
			fields = append(fields, record.Field{Name: c.name, Value: v})
		}
		if bad != nil {
			// The row goes to an error link as text, or stops the pipeline.
			failed++
			if err := out.Fail(raw(cols, row), badField, bad); err != nil {
				return err
			}
			continue
		}
		if err := out.Emit(record.FromFields(fields)); err != nil {
			return err
		}
	}
	if failed > 0 {
		out.Logf(levelInfo, "read %d %s from %s, %d with errors", rows, plural(rows, "row"), t.Path, failed)
	} else {
		out.Logf(levelInfo, "read %d %s from %s", rows, plural(rows, "row"), t.Path)
	}
	return nil
}

func value(row []string, i int) string {
	if i < len(row) {
		return row[i]
	}
	return ""
}

// raw returns the values of a row as text, for an error link.
func raw(cols []column, row []string) record.Record {
	fields := make([]record.Field, len(cols))
	for i, c := range cols {
		fields[i] = record.Field{Name: c.name, Value: value(row, c.index)}
	}
	return record.FromFields(fields)
}

// column is a column of the file read into a field.
type column struct {
	index int
	name  string
	codec codec
}

// columns decides from the first row which column goes into which field.
func (t *input) columns(first []string, header bool) ([]column, error) {
	if len(t.Fields) == 0 {
		cols := make([]column, len(first))
		seen := map[string]bool{}
		for i, h := range first {
			name := "field" + strconv.Itoa(i+1)
			if header && strings.TrimSpace(h) != "" {
				name = strings.TrimSpace(h)
			}
			if seen[name] {
				return nil, fmt.Errorf("%s:1: the column %q appears twice in the header; set fields: to choose the columns", t.Path, name)
			}
			seen[name] = true
			cols[i] = column{index: i, name: name, codec: textCodec}
		}
		return cols, nil
	}
	cols := make([]column, len(t.Fields))
	for i, f := range t.Fields {
		cols[i] = column{index: i, name: f.Name, codec: t.codecs[i]}
		if !header {
			continue
		}
		cols[i].index = -1
		for j, h := range first {
			if strings.TrimSpace(h) == f.Name {
				cols[i].index = j
				break
			}
		}
		if cols[i].index < 0 {
			return nil, fmt.Errorf("%s:1: the header has no column %q%s (columns: %s)", t.Path, f.Name,
				model.Suggest(f.Name, first), strings.Join(first, ", "))
		}
	}
	return cols, nil
}

// readError places an error of the CSV reader in the file.
func (t *input) readError(err error) error {
	var pe *stdcsv.ParseError
	if errors.As(err, &pe) {
		msg := pe.Err.Error()
		if errors.Is(pe.Err, stdcsv.ErrFieldCount) {
			msg = "the row has a different number of values than the first row"
		}
		return fmt.Errorf("%s:%d:%d: %s", t.Path, pe.Line, pe.Column, msg)
	}
	return fmt.Errorf("%s: %w", t.Path, err)
}
