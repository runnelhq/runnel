package excel

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"

	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// excel.output writes records to a sheet of a workbook and passes them on (S67b).
var outputDefinition = transform.Definition{
	Type:         "excel.output",
	RecordErrors: true,
	Category:     transform.CategoryOutput,
	Label:        "Excel Output",
	Description:  "Writes records to a sheet of an Excel workbook (.xlsx) and passes them on",
	Example:      "path: out/orders.xlsx\nsheet: Orders\nfields:\n  - { name: Nr }\n  - { name: Amount, format: \"#,##0.00\" }\n  - { name: Date }\n",
	Config:       output{},
	New:          newOutput,
}

type output struct {
	Path     string     `json:"path" doc:"Workbook to write (.xlsx or .xlsm), relative to the project folder or absolute" schema:"required,example=out/orders.xlsx"`
	Sheet    string     `json:"sheet" doc:"Name of the sheet; empty: Sheet1" schema:"example=Orders"`
	Header   *bool      `json:"header" doc:"Whether the first row holds the names of the columns, bold and frozen" schema:"default=true"`
	IfExists string     `json:"ifExists" doc:"If the workbook exists: replace it once all records are written, fail, or replace only the sheet (sheet) and keep the other sheets, charts and formulas" schema:"enum=replace|fail|sheet,default=replace"`
	Fields   []outField `json:"fields" doc:"Columns to write, in this order, with a number format; empty: all fields of the records"`

	cols []string // columns known before the run (fields or a closed schema)
}

// outField is a column of excel.output.
type outField struct {
	Name   string `json:"name" doc:"Name of the field to write (and of the column in the header)" schema:"required,format=runnel-field,example=amount"`
	Format string `json:"format" doc:"Number format of Excel as in English Excel, e.g. #,##0.00, 0.0% or dd.mm.yyyy hh:mm; empty: numbers as General, dates as short date" schema:"example=0.0%"`
}

// Limits of a sheet in Excel.
const (
	maxRows      = 1048576
	maxColumns   = 16384
	maxCellChars = 32767
	// widthSample is the number of records the widths of the columns are measured on;
	// they are kept until the widths are written, before the first row.
	widthSample = 100
)

func newOutput(with map[string]any) (transform.Transform, error) {
	t := &output{}
	if err := transform.DecodeConfig(with, t); err != nil {
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
		t.cols = append(t.cols, f.Name)
	}
	if len(t.cols) > maxColumns {
		return nil, fmt.Errorf("fields: a sheet has at most %d columns", maxColumns)
	}
	if !hasVar(t.IfExists) && t.IfExists != "" && t.IfExists != "replace" && t.IfExists != "fail" && t.IfExists != "sheet" {
		return nil, fmt.Errorf("invalid ifExists %q (expected replace, fail or sheet)", t.IfExists)
	}
	if !hasVar(t.Sheet) {
		if err := checkSheetName(t.Sheet); err != nil {
			return nil, err
		}
	}
	if !hasVar(t.Path) && t.Path != "" {
		if err := checkExtension(t.Path); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// checkSheetName checks a name as Excel does: at most 31 characters, none of : \ / ? * [ ].
func checkSheetName(name string) error {
	switch {
	case utf8.RuneCountInString(name) > 31:
		return fmt.Errorf("sheet: %q is longer than 31 characters, which Excel allows for a sheet name", name)
	case strings.ContainsAny(name, `:\/?*[]`):
		return fmt.Errorf(`sheet: %q contains one of : \ / ? * [ ], which Excel does not allow in a sheet name`, name)
	case strings.HasPrefix(name, "'") || strings.HasSuffix(name, "'"):
		return fmt.Errorf("sheet: %q starts or ends with ', which Excel does not allow", name)
	}
	return nil
}

func checkExtension(path string) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".xlsx", ".xlsm":
		return nil
	}
	return fmt.Errorf("path: %q is not a workbook ending in .xlsx or .xlsm", path)
}

// Init checks the fields to write against the incoming schema. Without fields: the
// columns are the fields of a closed schema, or else those of the first record.
func (t *output) Init(ic *transform.InitContext) (record.Schema, error) {
	for i, f := range t.Fields {
		ic.Use(f.Name, fmt.Sprintf("fields[%d].name", i))
	}
	if len(t.Fields) == 0 && !ic.In.Open {
		t.cols = ic.In.Names()
	}
	return ic.In, nil
}

func (t *output) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.Path == "" {
		return errors.New("path: no file given")
	}
	if err := checkExtension(t.Path); err != nil {
		return err
	}
	if err := checkSheetName(t.Sheet); err != nil {
		return err
	}
	sheet := t.Sheet
	if sheet == "" {
		sheet = "Sheet1"
	}
	file := paths.Resolve(ctx, t.Path)
	w, err := t.open(file, sheet)
	if err != nil {
		return err
	}
	defer func() { _ = w.f.Close() }()
	n, err := t.write(ctx, w, in, out)
	if err != nil {
		return err
	}
	if err := w.save(file); err != nil {
		return fmt.Errorf("cannot write %q: %w", t.Path, err)
	}
	out.Logf(slog.LevelInfo, "wrote %d %s to %s, sheet %s", n, plural(n, "row"), t.Path, sheet)
	return nil
}

// book is the workbook being written and the styles of its cells.
type book struct {
	f                      *excelize.File
	sw                     *excelize.StreamWriter
	header, date, dateTime int
	formats                map[string]int // style by number format of a field
	codes                  map[int]string // number format by style
}

// open starts the workbook: a new one, or for ifExists: sheet the existing one with the
// sheet emptied (or added).
func (t *output) open(file, sheet string) (*book, error) {
	info, err := os.Stat(file)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("cannot write %q: %w", t.Path, plainErr(err))
	}
	if exists && info.IsDir() {
		return nil, fmt.Errorf("cannot write %q: it is a folder", t.Path)
	}
	if exists && t.IfExists == "fail" {
		return nil, fmt.Errorf("%q already exists (ifExists: fail)", t.Path)
	}
	var f *excelize.File
	err = nil
	if exists && t.IfExists == "sheet" {
		// Limits for the workbook read into memory (ZIP bombs).
		if f, err = excelize.OpenFile(file, excelize.Options{UnzipSizeLimit: 1 << 30, UnzipXMLSizeLimit: 256 << 20}); err != nil {
			return nil, fmt.Errorf("cannot read %q to replace the sheet %q: %w", t.Path, sheet, openError(err))
		}
		if i, _ := f.GetSheetIndex(sheet); i < 0 {
			_, err = f.NewSheet(sheet)
		}
		if err == nil {
			// Formulas on other sheets that use the data are calculated again when Excel opens it.
			full := true
			err = f.SetCalcProps(&excelize.CalcPropsOptions{FullCalcOnLoad: &full})
		}
	} else {
		f = excelize.NewFile()
		if sheet != "Sheet1" {
			err = f.SetSheetName("Sheet1", sheet)
		}
	}
	b := &book{f: f, formats: map[string]int{}, codes: map[int]string{}}
	if err == nil {
		b.sw, err = f.NewStreamWriter(sheet)
	}
	if err == nil {
		b.header, err = f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	}
	if err == nil {
		b.date, err = f.NewStyle(&excelize.Style{NumFmt: 14})
	}
	if err == nil {
		b.dateTime, err = f.NewStyle(&excelize.Style{NumFmt: 22})
	}
	for i := 0; err == nil && i < len(t.Fields); i++ {
		if code := t.Fields[i].Format; code != "" && b.formats[code] == 0 {
			b.formats[code], err = f.NewStyle(&excelize.Style{CustomNumFmt: &code})
			b.codes[b.formats[code]] = code
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("cannot write %q: %w", t.Path, err)
	}
	return b, nil
}

// write writes the header and the records and passes the records on. The first records
// are kept until the widths of the columns are known.
func (t *output) write(ctx context.Context, b *book, in <-chan transform.Batch, out *transform.Output) (int64, error) {
	header := t.Header == nil || *t.Header
	formats := map[string]string{}
	for _, f := range t.Fields {
		formats[f.Name] = f.Format
	}
	cols := t.cols
	var sample []record.Record
	var rows [][]any // the cells of the sample
	row := 1
	var n, seen int64
	started := false
	start := func() error {
		started = true
		if err := t.layout(b, cols, rows, header); err != nil {
			return err
		}
		if header {
			cells := make([]any, len(cols))
			for i, c := range cols {
				cells[i] = excelize.Cell{StyleID: b.header, Value: c}
			}
			if err := b.sw.SetRow("A1", cells); err != nil {
				return err
			}
			row++
		}
		for i, r := range sample {
			if err := b.setRow(row, rows[i]); err != nil {
				return err
			}
			row++
			n++
			if err := out.Emit(r); err != nil {
				return err
			}
		}
		sample, rows = nil, nil
		return nil
	}
	skipped := map[string]bool{}
	for batch := range in {
		for _, r := range batch {
			if cols == nil {
				cols = r.Names() // open schema without fields: the first record decides
				if len(cols) > maxColumns {
					return n, fmt.Errorf("%s: the records have %d fields, a sheet has at most %d columns", t.Path, len(cols), maxColumns)
				}
			}
			seen++
			cells, field, err := b.cells(r, cols, formats)
			if err != nil {
				// Not written: to an error link, or the pipeline stops.
				if err := out.Fail(r, field, fmt.Errorf("%s: record %d, field %q: %w", t.Path, seen, field, err)); err != nil {
					return n, err
				}
				continue
			}
			if len(t.Fields) == 0 && r.Len() > len(cols) {
				for _, name := range r.Names() {
					if !skipped[name] && !slices.Contains(cols, name) {
						skipped[name] = true
						out.Logf(slog.LevelWarn, "the field %q is not written: the first record has no such field; list the columns with fields:", name)
					}
				}
			}
			if !started {
				sample, rows = append(sample, r), append(rows, cells)
				if len(sample) < widthSample {
					continue
				}
				if err := start(); err != nil {
					return n, t.writeError(err)
				}
				continue
			}
			if row > maxRows {
				return n, fmt.Errorf("%s: a sheet has at most %d rows", t.Path, maxRows)
			}
			if err := b.setRow(row, cells); err != nil {
				return n, t.writeError(err)
			}
			row++
			n++
			if err := out.Emit(r); err != nil {
				return n, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return n, err
	}
	if !started {
		if err := start(); err != nil {
			return n, t.writeError(err)
		}
	}
	if err := b.sw.Flush(); err != nil {
		return n, t.writeError(err)
	}
	return n, nil
}

func (t *output) writeError(err error) error {
	if strings.HasPrefix(err.Error(), t.Path) {
		return err
	}
	return fmt.Errorf("cannot write %q: %w", t.Path, err)
}

// layout sets the widths of the columns from the header and the first rows (about one
// unit per character, between 8 and 60) and freezes the header.
func (t *output) layout(b *book, cols []string, rows [][]any, header bool) error {
	for i, c := range cols {
		width := 0
		if header {
			width = utf8.RuneCountInString(c) + 2 // bold
		}
		for _, cells := range rows {
			width = max(width, b.width(cells[i]))
		}
		if err := b.sw.SetColWidth(i+1, i+1, float64(min(max(width+1, 8), 60))); err != nil {
			return err
		}
	}
	if header && len(cols) > 0 {
		return b.sw.SetPanes(&excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft",
			Selection: []excelize.Selection{{SQRef: "A2", ActiveCell: "A2", Pane: "bottomLeft"}}})
	}
	return nil
}

// width estimates the characters a cell shows: text by its longest line, a date with
// its format, a number with thousands separators or as long as its number format.
func (b *book) width(v any) int {
	style := 0
	if c, ok := v.(excelize.Cell); ok {
		v, style = c.Value, c.StyleID
	}
	switch v := v.(type) {
	case nil:
		return 0
	case string:
		w := 0
		for line := range strings.SplitSeq(v, "\n") {
			w = max(w, utf8.RuneCountInString(line))
		}
		return w
	case bool:
		return 5
	}
	if code := b.codes[style]; code != "" {
		return utf8.RuneCountInString(strings.NewReplacer(`"`, "", `\`, "", "_", "", "*", "").Replace(code)) + 1
	}
	switch style {
	case b.date:
		return 10
	case b.dateTime:
		return 16
	}
	n := len(fmt.Sprint(v))
	return n + n/3 + 1 // separators
}

func (b *book) setRow(row int, cells []any) error {
	name, err := excelize.CoordinatesToCellName(1, row)
	if err != nil {
		return err
	}
	return b.sw.SetRow(name, cells)
}

// cells converts the fields of a record into cells: text, numbers and true/false as they
// are, dates with a date format; lists and objects as JSON text. Text is never a formula.
func (b *book) cells(r record.Record, cols []string, formats map[string]string) ([]any, string, error) {
	cells := make([]any, len(cols))
	for i, c := range cols {
		v, _ := r.GetAt(i, c)
		style := 0
		if code := formats[c]; code != "" {
			style = b.formats[code]
		}
		var val any
		switch x := v.(type) {
		case nil:
		case string:
			if n := utf8.RuneCountInString(x); n > maxCellChars {
				return nil, c, fmt.Errorf("a cell holds at most %d characters, not %d", maxCellChars, n)
			}
			val = x
		case int64, bool:
			val = x
		case float64:
			if math.IsNaN(x) || math.IsInf(x, 0) {
				return nil, c, fmt.Errorf("%v cannot be written to Excel", x)
			}
			val = x
		case decimal.Decimal:
			val = x.InexactFloat64() // Excel stores numbers with about 15 digits
		case record.Date:
			if x.Year < 1900 {
				return nil, c, fmt.Errorf("%s: Excel has no dates before 1900", x)
			}
			val = time.Date(x.Year, x.Month, x.Day, 0, 0, 0, 0, time.UTC)
			style = cmp(style, b.date)
		case time.Time:
			if x.Year() < 1900 {
				return nil, c, fmt.Errorf("%s: Excel has no dates before 1900", x.Format(time.DateOnly))
			}
			// Excel has no time zones: the wall-clock time, as excelize counts in UTC.
			val = time.Date(x.Year(), x.Month(), x.Day(), x.Hour(), x.Minute(), x.Second(), x.Nanosecond(), time.UTC)
			style = cmp(style, b.dateTime)
		default:
			s := expr.Format(x)
			if utf8.RuneCountInString(s) > maxCellChars {
				return nil, c, fmt.Errorf("a cell holds at most %d characters", maxCellChars)
			}
			val = s
		}
		if style != 0 {
			cells[i] = excelize.Cell{StyleID: style, Value: val}
		} else {
			cells[i] = val
		}
	}
	return cells, "", nil
}

// cmp returns the style of the field, else the default one.
func cmp(field, def int) int {
	if field != 0 {
		return field
	}
	return def
}

// save writes the workbook to a temporary file next to the target and renames it, so
// that a failed run leaves the old file as it was.
func (b *book) save(file string) error {
	dir, base := filepath.Split(file)
	if dir == "" {
		dir = "."
	}
	tmp, err := os.CreateTemp(dir, "."+base+".*.tmp")
	if err != nil {
		return plainErr(err)
	}
	b.f.Path = file // the content type follows the extension (.xlsm)
	_, err = b.f.WriteTo(tmp)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		if err = os.Rename(tmp.Name(), file); err != nil && locked(err) {
			err = errors.New("the file is open in another program, e.g. Excel; close it and run again")
		}
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return plainErr(err)
	}
	return nil
}

// openError describes why an existing workbook cannot be read.
func openError(err error) error {
	switch {
	case errors.Is(err, excelize.ErrWorkbookFileFormat), strings.Contains(err.Error(), "zip: not a valid zip file"):
		return errors.New("it is not an Excel workbook (.xlsx)")
	case strings.Contains(err.Error(), "password"), errors.Is(err, excelize.ErrOptionsUnzipSizeLimit):
		return err
	}
	return plainErr(err)
}

// plainErr returns the reason of a file system error without the operation and path.
func plainErr(err error) error {
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return pe.Err
	}
	if le, ok := errors.AsType[*os.LinkError](err); ok {
		return le.Err
	}
	return err
}
