// Package excel contains the transforms of the namespace "excel" (roadmap S67):
// excel.input reads a sheet of a workbook (.xlsx, .xlsm) with its own reader
// (internal/xlsx), excel.output writes one with excelize. Each transform lives in a file
// of its own; this file holds the reading of a table that excel.input and its field
// detection share.
package excel

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/xlsx"
)

// area is the part of a sheet that holds the table (range:), 1-based; 0 means open.
type area struct{ col, row, endCol, endRow int }

// parseRange reads A3, A3:F, B2:D100 or A:F.
func parseRange(s string) (area, error) {
	if s == "" {
		return area{}, nil
	}
	bad := fmt.Errorf("range: %q is not a range like A3, A3:F or B2:D100", s)
	from, to, two := strings.Cut(strings.TrimSpace(s), ":")
	col, row, ok := xlsx.ParseCell(from)
	if !ok {
		return area{}, bad
	}
	a := area{col: col, row: row}
	if two {
		if a.endCol, a.endRow, ok = xlsx.ParseCell(to); !ok || a.endCol < a.col || (a.endRow != 0 && a.endRow < a.row) {
			return area{}, bad
		}
	}
	return a, nil
}

// table reads the rows of a table: the sheet, the header and the columns.
type table struct {
	book  *xlsx.Book
	rows  *xlsx.Rows
	sheet string
	where string // file and sheet for messages: data/orders.xlsx, sheet Orders
	area  area
	// names are the names of the columns from col to col+len(names)-1.
	names     []string
	header    int // row of the header, 0 without
	stopEmpty bool
	last      int // row read last
	pending   bool
}

// openTable opens the workbook and the sheet and reads the header row.
func openTable(ctx context.Context, path, sheet, rng string, header, stopEmpty bool) (*table, error) {
	a, err := parseRange(rng)
	if err != nil {
		return nil, err
	}
	book, err := xlsx.Open(paths.Resolve(ctx, path))
	if err != nil {
		if pe, ok := errors.AsType[*fs.PathError](err); ok {
			err = pe.Err
		}
		return nil, fmt.Errorf("cannot read %q: %w", path, err)
	}
	s, err := pickSheet(book, sheet)
	if err == nil {
		var rows *xlsx.Rows
		if rows, err = book.Rows(s); err == nil {
			t := &table{book: book, rows: rows, sheet: s.Name, where: path + ", sheet " + s.Name, area: a, stopEmpty: stopEmpty}
			if err = t.start(header); err == nil {
				return t, nil
			}
			_ = rows.Close()
		}
	}
	_ = book.Close()
	if !strings.HasPrefix(err.Error(), path) {
		err = fmt.Errorf("%s: %w", path, err)
	}
	return nil, err
}

func (t *table) close() {
	_ = t.rows.Close()
	_ = t.book.Close()
}

// pickSheet finds a sheet by name (case does not matter, as in Excel) or by number.
func pickSheet(b *xlsx.Book, name string) (xlsx.Sheet, error) {
	sheets := b.Sheets()
	if name == "" {
		return sheets[0], nil
	}
	names := make([]string, len(sheets))
	for i, s := range sheets {
		if strings.EqualFold(s.Name, name) {
			return s, nil
		}
		names[i] = s.Name
	}
	if n, err := strconv.Atoi(name); err == nil && n >= 1 && n <= len(sheets) {
		return sheets[n-1], nil
	}
	return xlsx.Sheet{}, fmt.Errorf("the workbook has no sheet %q%s (sheets: %s)", name, model.Suggest(name, names), strings.Join(names, ", "))
}

// start finds the first row of the table and reads the names of the columns: from the
// header, else field1, field2 … Without range the table starts at the first cell of the
// first row with values; without end column it ends with the last cell of that row.
func (t *table) start(header bool) error {
	for t.rows.Next() {
		n, cells := t.rows.Row()
		if n < t.area.row {
			continue
		}
		if t.area.endRow != 0 && n > t.area.endRow {
			break
		}
		cells = t.within(cells)
		if len(cells) == 0 {
			continue
		}
		if t.area.col == 0 {
			t.area.col = cells[0].Col
		}
		end := t.area.endCol
		if end == 0 {
			end = cells[len(cells)-1].Col
		}
		t.names = make([]string, end-t.area.col+1)
		for i := range t.names {
			t.names[i] = "field" + strconv.Itoa(i+1)
		}
		if !header {
			t.pending, t.last = true, n-1
			return nil
		}
		t.header, t.last = n, n
		seen := map[string]bool{}
		for _, c := range cells {
			if c.Col > end {
				break
			}
			name := strings.TrimSpace(text(c.Value))
			if c.Error != "" || name == "" {
				continue
			}
			if seen[name] {
				return fmt.Errorf("%s: the column %q appears twice in the header; set fields: to choose the columns", t.cell(c.Col, n), name)
			}
			seen[name] = true
			t.names[c.Col-t.area.col] = name
		}
		return nil
	}
	if err := t.rows.Err(); err != nil {
		return err
	}
	return nil // an empty table: no columns, no rows
}

// within returns the cells inside the columns of the range.
func (t *table) within(cells []xlsx.Cell) []xlsx.Cell {
	for len(cells) > 0 && cells[0].Col < t.area.col {
		cells = cells[1:]
	}
	if t.area.endCol != 0 {
		for len(cells) > 0 && cells[len(cells)-1].Col > t.area.endCol {
			cells = cells[:len(cells)-1]
		}
	}
	return cells
}

// next returns the next row of the table with its number and the cells inside its
// columns; false at the end of the table or on an error (t.rows.Err()).
func (t *table) next() (int, []xlsx.Cell, bool) {
	for t.pending || t.rows.Next() {
		t.pending = false
		n, cells := t.rows.Row()
		if t.area.endRow != 0 && n > t.area.endRow {
			return 0, nil, false
		}
		end := t.area.col + len(t.names) - 1
		for len(cells) > 0 && cells[0].Col < t.area.col {
			cells = cells[1:]
		}
		for len(cells) > 0 && cells[len(cells)-1].Col > end {
			cells = cells[:len(cells)-1]
		}
		if len(cells) == 0 || (t.stopEmpty && n > t.last+1) {
			if t.stopEmpty {
				return 0, nil, false
			}
			continue
		}
		t.last = n
		return n, cells, true
	}
	return 0, nil, false
}

// cell names a cell for messages, e.g. data/orders.xlsx, sheet Orders, cell C5.
func (t *table) cell(col, row int) string {
	return t.where + ", cell " + xlsx.ColumnName(col) + strconv.Itoa(row)
}
