package xlsx

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
)

// Cell is a cell with a value.
type Cell struct {
	// Col is the number of the column, 1 for A.
	Col int
	// Value is a string, int64, decimal.Decimal, bool, record.Date or time.Time; nil for
	// a formula without a calculated value. Times without a date are text (08:30:00).
	Value any
	// Error is the error value of a cell, e.g. #DIV/0! or #N/A; Value is then nil.
	Error string
}

// Rows reads the rows of a sheet one after another.
type Rows struct {
	b     *Book
	sheet string
	rc    io.ReadCloser
	d     *xml.Decoder
	num   int
	cells []Cell
	err   error
	done  bool
}

// Rows starts reading a sheet. Close it when done.
func (b *Book) Rows(s Sheet) (*Rows, error) {
	if s.part == "" {
		return nil, fmt.Errorf("the workbook is damaged: the sheet %q has no data", s.Name)
	}
	rc, err := b.open(s.part, maxSheetSize)
	if err != nil {
		return nil, err
	}
	return &Rows{b: b, sheet: s.Name, rc: rc, d: xml.NewDecoder(rc)}, nil
}

// Close ends reading.
func (r *Rows) Close() error { return r.rc.Close() }

// Err returns the error that ended reading, if any.
func (r *Rows) Err() error { return r.err }

// Row returns the number of the current row (1 for the first) and its cells with values,
// in the order of the columns. The cells are valid until the next call of Next.
func (r *Rows) Row() (int, []Cell) { return r.num, r.cells }

// Next reads the next row that has cells. Rows without cells are not returned; their
// numbers show the gap.
func (r *Rows) Next() bool {
	if r.done {
		return false
	}
	r.cells = r.cells[:0]
	for {
		tok, err := r.d.RawToken()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF // before </sheetData>
			}
			return r.fail(err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				n := r.num + 1
				if v := attr(t, "r"); v != "" {
					if n, err = strconv.Atoi(v); err != nil || n < 1 || n > maxRows || n <= r.num {
						return r.fail(fmt.Errorf("the workbook is damaged: invalid row number %q after row %d", v, r.num))
					}
				}
				if n > maxRows {
					return r.fail(fmt.Errorf("the sheet has more than %d rows", maxRows))
				}
				r.num = n
				r.cells = r.cells[:0]
			case "c":
				if err := r.cell(t); err != nil {
					return r.fail(err)
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "row":
				if len(r.cells) > 0 {
					return true
				}
			case "sheetData":
				r.done = true
				return false
			}
		}
	}
}

// fail ends reading with an error that names the sheet.
func (r *Rows) fail(err error) bool {
	if se, ok := errors.AsType[*xml.SyntaxError](err); ok {
		err = fmt.Errorf("the workbook is damaged: line %d: %s", se.Line, se.Msg)
	} else if errors.Is(err, io.ErrUnexpectedEOF) {
		err = errors.New("the workbook is damaged: the sheet ends in the middle")
	}
	r.err, r.done = fmt.Errorf("sheet %q: %w", r.sheet, err), true
	return false
}

// cell reads a cell <c r="B2" t="s" s="1"><v>…</v></c> up to its end.
func (r *Rows) cell(start xml.StartElement) error {
	col := 1
	if len(r.cells) > 0 {
		col = r.cells[len(r.cells)-1].Col + 1
	}
	ref, typ, style := attr(start, "r"), attr(start, "t"), attr(start, "s")
	if ref != "" {
		c, row, ok := ParseCell(ref)
		if !ok || (row != 0 && row != r.num) {
			return fmt.Errorf("the workbook is damaged: invalid cell %q in row %d", ref, r.num)
		}
		col = c
	}
	if col > maxColumns || (len(r.cells) > 0 && col <= r.cells[len(r.cells)-1].Col) {
		return fmt.Errorf("the workbook is damaged: invalid cell %q in row %d", ref, r.num)
	}
	var v []byte
	text, hasText := "", false
	for {
		tok, err := r.d.RawToken()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "v":
				if v, err = r.chars("v"); err != nil {
					return err
				}
			case "is":
				if text, err = richText(r.d, "is"); err != nil {
					return err
				}
				hasText = true
			}
		case xml.EndElement:
			if t.Name.Local != "c" {
				continue
			}
			c, err := r.value(col, typ, style, v, text, hasText)
			if err != nil {
				return fmt.Errorf("cell %s%d: %w", ColumnName(col), r.num, err)
			}
			if c.Value != nil || c.Error != "" {
				r.cells = append(r.cells, c)
			}
			return nil
		}
	}
}

// chars reads the text of an element up to its end.
func (r *Rows) chars(end string) ([]byte, error) {
	var out []byte
	for {
		tok, err := r.d.RawToken()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return nil, err
		}
		switch t := tok.(type) {
		case xml.CharData:
			out = append(out, t...)
		case xml.EndElement:
			if t.Name.Local == end {
				return out, nil
			}
		}
	}
}

// value converts the content of a cell by its type (t="…") and format (s="…").
func (r *Rows) value(col int, typ, style string, v []byte, text string, hasText bool) (Cell, error) {
	c := Cell{Col: col}
	raw := strings.TrimSpace(string(v))
	switch typ {
	case "s":
		if raw == "" {
			return c, nil
		}
		i, err := strconv.Atoi(raw)
		if err != nil || i < 0 || i >= len(r.b.strings) {
			return c, fmt.Errorf("the workbook is damaged: no shared text %q", raw)
		}
		c.Value = r.b.strings[i]
	case "inlineStr":
		if hasText {
			c.Value = text
		} else if len(v) > 0 {
			c.Value = unescape(string(v))
		}
	case "str":
		c.Value = unescape(string(v)) // the text result of a formula
	case "b":
		switch raw {
		case "1", "true":
			c.Value = true
		case "0", "false":
			c.Value = false
		case "":
		default:
			return c, fmt.Errorf("the workbook is damaged: %q is not true or false", raw)
		}
	case "e":
		c.Error = raw
	case "d":
		if raw == "" {
			return c, nil
		}
		val, err := isoDate(raw)
		if err != nil {
			return c, err
		}
		c.Value = val
	case "", "n":
		if raw == "" {
			return c, nil
		}
		n, err := decimal.NewFromString(raw)
		if err != nil {
			return c, fmt.Errorf("the workbook is damaged: %q is not a number", raw)
		}
		if k := r.kind(style); k != kindNumber {
			val, err := serial(n, k, r.b.date1904)
			if err != nil {
				return c, err
			}
			c.Value = val
			return c, nil
		}
		c.Value = number(n)
	default:
		return c, fmt.Errorf("the workbook is damaged: unknown cell type %q", typ)
	}
	return c, nil
}

func (r *Rows) kind(style string) kind {
	if style == "" {
		return kindNumber
	}
	i, err := strconv.Atoi(style)
	if err != nil || i < 0 || i >= len(r.b.formats) {
		return kindNumber
	}
	return r.b.formats[i]
}

// number returns a number as Excel shows it: with at most 15 significant digits, so that
// 0.1+0.2 stored as 0.30000000000000004 is 0.3; whole numbers as int64.
func number(n decimal.Decimal) any {
	if digits := len(n.Coefficient().String()); digits > 15 {
		// Excel's smallest number is about 1E-307: smaller exponents are not rounded.
		if intDigits := digits + int(n.Exponent()); intDigits <= 15 && intDigits > -400 {
			n = n.Round(int32(15 - intDigits)) //nolint:gosec // between 0 and 415
		}
	}
	if n.IsInteger() && n.Abs().LessThan(decimal.New(1, 18)) {
		return n.IntPart()
	}
	return n
}

// isoDate reads a cell of type d: a date or date and time in ISO 8601.
func isoDate(s string) (any, error) {
	if d, err := record.ParseDate(s); err == nil {
		return d, nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05.999999999Z07:00", "15:04:05.999999999"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			if layout[0] == '1' {
				return t.Format("15:04:05"), nil
			}
			return t, nil
		}
	}
	return nil, fmt.Errorf("the workbook is damaged: %q is not a date", s)
}

// ParseCell reads a cell reference like B12 or $B$12 into column and row (1-based); a
// reference without row (B) has row 0.
func ParseCell(ref string) (col, row int, ok bool) {
	s := strings.ToUpper(strings.ReplaceAll(ref, "$", ""))
	i := 0
	for i < len(s) && s[i] >= 'A' && s[i] <= 'Z' {
		col = col*26 + int(s[i]-'A'+1)
		if col > maxColumns {
			return 0, 0, false
		}
		i++
	}
	if i == 0 {
		return 0, 0, false
	}
	if i == len(s) {
		return col, 0, true
	}
	n, err := strconv.Atoi(s[i:])
	if err != nil || n < 1 || n > maxRows || s[i] == '+' || s[i] == '-' {
		return 0, 0, false
	}
	return col, n, true
}

// ColumnName returns the letters of a column, e.g. 1 → A, 28 → AB.
func ColumnName(col int) string {
	var b []byte
	for col > 0 {
		col--
		b = append([]byte{byte('A' + col%26)}, b...)
		col /= 26
	}
	return string(b)
}
