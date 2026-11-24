package excel

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
	"github.com/runnelhq/runnel/internal/xlsx"
)

// orderRecords are records with all kinds of values.
func orderRecords() []record.Record {
	return []record.Record{
		record.New(record.Field{Name: "Nr", Value: int64(1001)}, record.Field{Name: "Kunde", Value: "Müller & Söhne"},
			record.Field{Name: "Betrag", Value: decimalOf("1234.5")}, record.Field{Name: "Datum", Value: dateOf("2026-11-19")},
			record.Field{Name: "Am", Value: time.Date(2026, 11, 19, 14, 5, 7, 0, time.Local)}, record.Field{Name: "Bezahlt", Value: true},
			record.Field{Name: "Notiz", Value: "=1+1"}),
		record.New(record.Field{Name: "Nr", Value: int64(1002)}, record.Field{Name: "Kunde", Value: "Zeile\numbruch"},
			record.Field{Name: "Betrag", Value: 0.1}, record.Field{Name: "Datum", Value: nil},
			record.Field{Name: "Am", Value: nil}, record.Field{Name: "Bezahlt", Value: false},
			record.Field{Name: "Notiz", Value: map[string]any{"a": int64(1)}}),
	}
}

// readBack reads all sheets of a workbook with the reader of excel.input.
func readBack(t *testing.T, path string) string {
	t.Helper()
	b, err := xlsx.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	var sb strings.Builder
	for _, s := range b.Sheets() {
		fmt.Fprintf(&sb, "[%s]\n", s.Name)
		r, err := b.Rows(s)
		if err != nil {
			t.Fatal(err)
		}
		for r.Next() {
			n, cells := r.Row()
			fmt.Fprintf(&sb, "%d:", n)
			for _, c := range cells {
				v := fmt.Sprintf("%v(%T)", c.Value, c.Value)
				if tm, ok := c.Value.(time.Time); ok {
					v = tm.Format("2006-01-02 15:04:05") + "(time.Time)"
				}
				fmt.Fprintf(&sb, " %s=%s", xlsx.ColumnName(c.Col), v)
			}
			sb.WriteString("\n")
		}
		if r.Err() != nil {
			t.Fatal(r.Err())
		}
		_ = r.Close()
	}
	return sb.String()
}

func write(t *testing.T, with map[string]any, records ...record.Record) ([]record.Record, []transformtest.Logged, error) {
	t.Helper()
	return transformtest.Run(context.Background(), transformtest.New(t, outputDefinition, with), records...)
}

func TestOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.xlsx")
	out, logs, err := write(t, map[string]any{"path": path, "sheet": "Bestellungen"}, orderRecords()...)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || len(logs) != 1 || logs[0].Msg != "wrote 2 rows to "+path+", sheet Bestellungen" {
		t.Errorf("out %d logs %v", len(out), logs)
	}
	// Values keep their type; a date with time is the wall-clock time; text is never a formula.
	want := `[Bestellungen]
1: A=Nr(string) B=Kunde(string) C=Betrag(string) D=Datum(string) E=Am(string) F=Bezahlt(string) G=Notiz(string)
2: A=1001(int64) B=Müller & Söhne(string) C=1234.5(decimal.Decimal) D=2026-11-19(record.Date) E=2026-11-19 14:05:07(time.Time) F=true(bool) G==1+1(string)
3: A=1002(int64) B=Zeile
umbruch(string) C=0.1(decimal.Decimal) F=false(bool) G={"a":1}(string)
`
	if got := readBack(t, path); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// The header is bold and frozen, the columns have widths.
	sheet := part(t, path, "xl/worksheets/sheet1.xml")
	for _, s := range []string{`state="frozen"`, `ySplit="1"`, `<col min="2" max="2" width="15"`, `<col min="5" max="5" width="17"`, `t="inlineStr"`} {
		if !strings.Contains(sheet, s) {
			t.Errorf("sheet without %s:\n%s", s, sheet)
		}
	}
	if strings.Contains(sheet, "<f>") {
		t.Errorf("a formula was written:\n%s", sheet)
	}
}

// Fields choose and order the columns and give number formats; without header the
// first row holds data.
func TestOutputFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.xlsm")
	_, _, err := write(t, map[string]any{"path": path, "header": false, "fields": []any{
		map[string]any{"name": "Betrag", "excelFormat": "#,##0.00 \"€\""},
		map[string]any{"name": "Datum", "format": "dd.MM.yyyy"},
		map[string]any{"name": "Nr"},
	}}, orderRecords()...)
	if err != nil {
		t.Fatal(err)
	}
	want := "[Sheet1]\n1: A=1234.5(decimal.Decimal) B=2026-11-19(record.Date) C=1001(int64)\n2: A=0.1(decimal.Decimal) C=1002(int64)\n"
	if got := readBack(t, path); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if styles := part(t, path, "xl/styles.xml"); (!strings.Contains(styles, `formatCode="#,##0.00 &#34;€&#34;"`) && !strings.Contains(styles, `formatCode="#,##0.00 &quot;€&quot;"`)) ||
		!strings.Contains(styles, `formatCode="dd.mm.yyyy"`) {
		t.Errorf("styles:\n%s", styles)
	}
	if ct := part(t, path, "[Content_Types].xml"); !strings.Contains(ct, "sheet.macroEnabled.main+xml") {
		t.Errorf(".xlsm without its content type:\n%s", ct)
	}
	if strings.Contains(part(t, path, "xl/worksheets/sheet1.xml"), "<pane") {
		t.Error("frozen pane without header")
	}
}

// ifExists: replace writes a new file only once all records are written; fail keeps
// the file; sheet replaces one sheet and keeps the others.
func TestOutputIfExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.xlsx")
	one := record.New(record.Field{Name: "n", Value: int64(1)})
	two := record.New(record.Field{Name: "n", Value: int64(2)})
	if _, _, err := write(t, map[string]any{"path": path, "sheet": "Cover"}, record.New(record.Field{Name: "title", Value: "Report"})); err != nil {
		t.Fatal(err)
	}
	if _, _, err := write(t, map[string]any{"path": path, "sheet": "Data", "ifExists": "sheet"}, one); err != nil {
		t.Fatal(err)
	}
	if _, _, err := write(t, map[string]any{"path": path, "sheet": "Data", "ifExists": "sheet"}, two); err != nil {
		t.Fatal(err)
	}
	if got := readBack(t, path); got != "[Cover]\n1: A=title(string)\n2: A=Report(string)\n[Data]\n1: A=n(string)\n2: A=2(int64)\n" {
		t.Errorf("sheet:\n%s", got)
	}
	if wb := part(t, path, "xl/workbook.xml"); !strings.Contains(wb, `fullCalcOnLoad="true"`) {
		t.Errorf("no recalculation on load:\n%s", wb)
	}
	if _, _, err := write(t, map[string]any{"path": path, "ifExists": "fail"}, one); err == nil || !strings.Contains(err.Error(), "already exists (ifExists: fail)") {
		t.Errorf("fail: %v", err)
	}
	if _, _, err := write(t, map[string]any{"path": path}, one); err != nil {
		t.Fatal(err)
	}
	if got := readBack(t, path); got != "[Sheet1]\n1: A=n(string)\n2: A=1(int64)\n" {
		t.Errorf("replace:\n%s", got)
	}
	// No temporary files are left.
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("files: %v", entries)
	}
}

// A value that does not fit goes to the error link; the other records are written.
func TestOutputErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.xlsx")
	tr := transformtest.New(t, outputDefinition, map[string]any{"path": path})
	out, failed, _, err := transformtest.RunWithErrorLink(context.Background(), tr,
		record.New(record.Field{Name: "v", Value: "ok"}),
		record.New(record.Field{Name: "v", Value: strings.Repeat("x", maxCellChars+1)}),
		record.New(record.Field{Name: "v", Value: math.Inf(1)}),
		record.New(record.Field{Name: "v", Value: dateOf("1899-12-31")}),
	)
	if err != nil || len(out) != 1 || len(failed) != 3 {
		t.Fatalf("%d %d %v", len(out), len(failed), err)
	}
	for i, want := range []string{"record 2, field \"v\": a cell holds at most 32767 characters, not 32768", "record 3, field \"v\": +Inf cannot be written", "Excel has no dates before 1900"} {
		if e, _ := failed[i].Get("_error"); !strings.Contains(fmt.Sprint(e), want) {
			t.Errorf("%d: %v", i, e)
		}
	}

	// Without an error link the pipeline stops and the old file stays.
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := write(t, map[string]any{"path": path}, record.New(record.Field{Name: "v", Value: math.NaN()})); err == nil || !strings.Contains(err.Error(), "NaN cannot be written to Excel") {
		t.Errorf("NaN: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "old" {
		t.Errorf("file replaced: %q", b)
	}
	// ifExists: sheet with a file that is no workbook.
	if _, _, err := write(t, map[string]any{"path": path, "ifExists": "sheet"}, record.New(record.Field{Name: "v", Value: "x"})); err == nil || !strings.Contains(err.Error(), "to replace the sheet \"Sheet1\": it is not an Excel workbook (.xlsx)") {
		t.Errorf("sheet: %v", err)
	}
	// A folder as target.
	folder := filepath.Join(dir, "folder.xlsx")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := write(t, map[string]any{"path": folder}, record.New()); err == nil || !strings.Contains(err.Error(), "it is a folder") {
		t.Errorf("folder: %v", err)
	}

	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"path": "out.csv"}, `path: "out.csv" is not a workbook ending in .xlsx or .xlsm`},
		{map[string]any{"path": "out.xls"}, `is not a workbook ending in .xlsx or .xlsm`},
		{map[string]any{"path": "a.xlsx", "sheet": "Q1/2026"}, `contains one of : \ / ? * [ ]`},
		{map[string]any{"path": "a.xlsx", "sheet": strings.Repeat("a", 32)}, `longer than 31 characters`},
		{map[string]any{"path": "a.xlsx", "ifExists": "append"}, `invalid ifExists "append" (expected replace, fail or sheet)`},
		{map[string]any{"path": "a.xlsx", "fields": []any{map[string]any{"name": "a"}, map[string]any{"name": "a"}}}, `the field "a" is listed twice`},
	} {
		if _, err := newOutput(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
}

// Without records the file has the header of the known columns; fields of later
// records that the first one does not have are reported.
func TestOutputColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.xlsx")
	tr := transformtest.New(t, outputDefinition, map[string]any{"path": path})
	if _, err := tr.(transform.Initializer).Init(transform.NewInitContext(record.Schema{}.With("a", record.String).With("b", record.Int))); err != nil {
		t.Fatal(err)
	}
	if _, _, err := transformtest.Run(context.Background(), tr); err != nil {
		t.Fatal(err)
	}
	if got := readBack(t, path); got != "[Sheet1]\n1: A=a(string) B=b(string)\n" {
		t.Errorf("got %q", got)
	}
	_, logs, err := write(t, map[string]any{"path": path}, record.New(record.Field{Name: "a", Value: "x"}),
		record.New(record.Field{Name: "a", Value: "y"}, record.Field{Name: "extra", Value: "z"}))
	if err != nil || len(logs) != 2 || !strings.Contains(logs[0].Msg, `the field "extra" is not written`) {
		t.Errorf("%v %v", logs, err)
	}
}

// part returns a part of a workbook.
func part(t *testing.T, path, name string) string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(rc)
			_ = rc.Close()
			return string(b)
		}
	}
	t.Fatalf("%s has no %s", path, name)
	return ""
}

// testdata/report.xlsx (LibreOffice): a cover sheet with a sum over Daten!B2:B100 and a
// chart of the sheet Daten. ifExists: sheet replaces the data and keeps the chart; the
// sum is calculated again when Excel opens the file.
func TestOutputSheetKeepsChart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.xlsx")
	src, err := os.ReadFile("testdata/report.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	var rows []record.Record
	for i := range 3 {
		rows = append(rows, record.New(record.Field{Name: "Kunde", Value: fmt.Sprintf("Neu %d", i)}, record.Field{Name: "Umsatz", Value: int64(100 + i)}))
	}
	if _, _, err := write(t, map[string]any{"path": path, "sheet": "Daten", "ifExists": "sheet"}, rows...); err != nil {
		t.Fatal(err)
	}
	got := readBack(t, path)
	if !strings.HasPrefix(got, "[Deckblatt]\n1: A=Bericht(string)\n") || !strings.HasSuffix(got, "[Daten]\n1: A=Kunde(string) B=Umsatz(string)\n2: A=Neu 0(string) B=100(int64)\n3: A=Neu 1(string) B=101(int64)\n4: A=Neu 2(string) B=102(int64)\n") {
		t.Errorf("got\n%s", got)
	}
	if !strings.Contains(part(t, path, "xl/charts/chart1.xml"), "Daten") || !strings.Contains(part(t, path, "xl/worksheets/sheet1.xml"), ">SUM(Daten!B2:B100)</f>") {
		t.Error("chart or formula lost")
	}
}

// More records than the sample for the widths: all are written in order and passed on.
func TestOutputManyRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "many.xlsx")
	var rows []record.Record
	for i := range 250 {
		rows = append(rows, record.New(record.Field{Name: "n", Value: int64(i)}))
	}
	out, _, err := write(t, map[string]any{"path": path, "header": false}, rows...)
	if err != nil || len(out) != 250 {
		t.Fatal(len(out), err)
	}
	b, _ := xlsx.Open(path)
	defer func() { _ = b.Close() }()
	r, _ := b.Rows(b.Sheets()[0])
	n := 0
	for r.Next() {
		row, cells := r.Row()
		if row != n+1 || cells[0].Value != int64(n) {
			t.Fatalf("row %d: %v", row, cells)
		}
		n++
	}
	if n != 250 {
		t.Errorf("%d rows", n)
	}
}

// BenchmarkOutput writes 10,000 records of 5 fields: text, whole and decimal numbers, a
// date and true/false.
func BenchmarkOutput(b *testing.B) {
	rows := make([]record.Record, 10000)
	day := dateOf("2026-11-23")
	for i := range rows {
		rows[i] = record.New(record.Field{Name: "Name", Value: fmt.Sprintf("Customer %d", i)}, record.Field{Name: "Nr", Value: int64(i)},
			record.Field{Name: "Amount", Value: decimalOf("1234.25")}, record.Field{Name: "Date", Value: day}, record.Field{Name: "Paid", Value: i%2 == 0})
	}
	path := filepath.Join(b.TempDir(), "bench.xlsx")
	tr, err := newOutput(map[string]any{"path": path, "fields": []any{map[string]any{"name": "Name"}, map[string]any{"name": "Nr"},
		map[string]any{"name": "Amount", "format": "#,##0.00"}, map[string]any{"name": "Date"}, map[string]any{"name": "Paid"}}})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if out, _, err := transformtest.Run(context.Background(), tr, rows...); err != nil || len(out) != 10000 {
			b.Fatal(len(out), err)
		}
	}
}
