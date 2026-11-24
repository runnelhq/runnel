package excel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

// testdata/orders.xlsx, written by LibreOffice:
//
//	Orders: a title in A1, the table from A3 (Nr, Kunde, Betrag, Datum, Bezahlt, Notiz);
//	        row 5 has #DIV/0! in Betrag, row 6 a date as text, row 7 is empty, row 8 a
//	        date as plain number; row 10 a sum below the table.
//	Raw:    B2:C3 without header.
const orders = "testdata/orders.xlsx"

// show writes records as one line each, with the Go type of every value.
func show(rs []record.Record) string {
	var b strings.Builder
	for _, r := range rs {
		for i, name := range r.Names() {
			v, _ := r.Get(name)
			if i > 0 {
				b.WriteString(" ")
			}
			text := fmt.Sprint(v)
			if t, ok := v.(time.Time); ok {
				text = t.Format("2006-01-02 15:04")
			}
			fmt.Fprintf(&b, "%s=%s(%T)", name, text, v)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func run(t *testing.T, with map[string]any) (out, failed []record.Record, logs []transformtest.Logged, err error) {
	t.Helper()
	return transformtest.RunWithErrorLink(context.Background(), transformtest.New(t, inputDefinition, with))
}

// Without fields every column of the table, with the values of its cells; a row with an
// error cell goes to the error link.
func TestInputAllColumns(t *testing.T) {
	out, failed, logs, err := run(t, map[string]any{"path": orders, "range": "A3"})
	if err != nil {
		t.Fatal(err)
	}
	want := `Nr=1(int64) Kunde=Müller(string) Betrag=12.5(decimal.Decimal) Datum=2026-11-02(record.Date) Bezahlt=true(bool) Notiz=<nil>(<nil>)
Nr=3(int64) Kunde=Ärger(string) Betrag=7(int64) Datum=05.11.2026(string) Bezahlt=true(bool) Notiz=<nil>(<nil>)
Nr=4(int64) Kunde=Weber(string) Betrag=0.1(decimal.Decimal) Datum=46333(int64) Bezahlt=false(bool) Notiz=<nil>(<nil>)
`
	if got := show(out); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// Row 5 and the sum below the table, which counts the error in.
	if len(failed) != 2 || !strings.Contains(show(failed[:1]), "Nr=2(int64) Kunde=Schmidt(string) Betrag=#DIV/0!(string) Datum=2026-11-03(record.Date)") ||
		!strings.Contains(show(failed[1:]), "Nr=Summe(string) Kunde=<nil>(<nil>) Betrag=#DIV/0!(string)") {
		t.Errorf("failed %s", show(failed))
	}
	if e, _ := failed[0].Get("_error"); e != "testdata/orders.xlsx, sheet Orders, cell C5: the cell shows the error #DIV/0!" {
		t.Errorf("error %v", e)
	}
	if f, _ := failed[0].Get("_errorField"); f != "Betrag" {
		t.Errorf("field %v", f)
	}
	if len(logs) != 1 || logs[0].Msg != "read 5 rows from testdata/orders.xlsx, sheet Orders, 2 with errors" {
		t.Errorf("logs %v", logs)
	}

	// Without range the table starts with the title in A1: its only column.
	out, _, _, err = run(t, map[string]any{"path": orders, "emptyRows": "stop"})
	if err != nil || show(out) != "" {
		t.Errorf("title: %s %v", show(out), err)
	}
}

// Fields select and convert columns: dates as text with a format, numbers in a date
// column by the date system of Excel; emptyRows: stop ends the table before the sum.
func TestInputFields(t *testing.T) {
	out, failed, _, err := run(t, map[string]any{"path": orders, "sheet": "orders", "range": "A3", "emptyRows": "stop", "fields": []any{
		map[string]any{"name": "Datum", "type": "date", "format": "dd.MM.yyyy"},
		map[string]any{"name": "Nr", "type": "string"},
		map[string]any{"name": "Kunde"},
		map[string]any{"name": "Bezahlt", "type": "bool"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := `Datum=2026-11-02(record.Date) Nr=1(string) Kunde=Müller(string) Bezahlt=true(bool)
Datum=2026-11-03(record.Date) Nr=2(string) Kunde=Schmidt(string) Bezahlt=false(bool)
Datum=2026-11-05(record.Date) Nr=3(string) Kunde=Ärger(string) Bezahlt=true(bool)
`
	if got := show(out); got != want || len(failed) != 0 {
		t.Errorf("got\n%s\nwant\n%s\nfailed %s", got, want, show(failed))
	}

	// Without stop: row 8 with a date as plain number, and the sum, which is no number.
	out, failed, _, err = run(t, map[string]any{"path": orders, "range": "A3:D", "fields": []any{
		map[string]any{"name": "Datum", "type": "datetime"},
		map[string]any{"name": "Nr", "type": "int"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := show(out); !strings.Contains(got, "Datum=2026-11-02 00:00(time.Time) Nr=1(int64)") || !strings.Contains(got, "Datum=2026-11-07 00:00(time.Time) Nr=4(int64)") || len(out) != 3 {
		t.Errorf("got\n%s", got)
	}
	if len(failed) != 2 || !strings.Contains(fmt.Sprint(failed[0].Get("_error")), `cell D6: field "Datum": `) ||
		!strings.Contains(fmt.Sprint(failed[1].Get("_error")), `cell A10: field "Nr": `) {
		t.Errorf("failed %s", show(failed))
	}
}

// A sheet by number, without header: the columns are field1, field2 … or the fields in
// their order; range ends the table.
func TestInputWithoutHeader(t *testing.T) {
	out, _, _, err := run(t, map[string]any{"path": orders, "sheet": "2", "header": false})
	if err != nil {
		t.Fatal(err)
	}
	if got := show(out); got != "field1=a(string) field2=1(int64)\nfield1=b(string) field2=2.5(decimal.Decimal)\n" {
		t.Errorf("got\n%s", got)
	}
	out, _, _, err = run(t, map[string]any{"path": orders, "sheet": "Raw", "header": false, "range": "B2:D2", "fields": []any{
		map[string]any{"name": "name"}, map[string]any{"name": "n", "type": "decimal"}, map[string]any{"name": "extra"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := show(out); got != "name=a(string) n=1(decimal.Decimal) extra=<nil>(<nil>)\n" {
		t.Errorf("got\n%s", got)
	}
}

func TestInputErrors(t *testing.T) {
	dir := t.TempDir()
	xls := filepath.Join(dir, "old.xls")
	if err := os.WriteFile(xls, append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, make([]byte, 600)...), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"path": filepath.Join(dir, "none.xlsx")}, `none.xlsx": `},
		{map[string]any{"path": xls}, "Excel 97–2003 workbook (.xls) or protected with a password"},
		{map[string]any{"path": orders, "sheet": "Order"}, `testdata/orders.xlsx: the workbook has no sheet "Order" (did you mean "Orders"?) (sheets: Orders, Raw)`},
		{map[string]any{"path": orders, "sheet": "3"}, `no sheet "3"`},
		{map[string]any{"path": orders, "range": "A3", "fields": []any{map[string]any{"name": "Betrg"}}},
			`testdata/orders.xlsx, sheet Orders, row 3: the header has no column "Betrg" (did you mean "Betrag"?) (columns: Nr, Kunde, Betrag, Datum, Bezahlt, Notiz)`},
		{map[string]any{"path": orders, "range": "${R}"}, `range: "${R}" is not a range`},
	} {
		_, _, _, err := run(t, tt.with)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
	// Errors in the configuration, before the run.
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"path": "a.xlsx", "range": "3A"}, `range: "3A" is not a range like A3, A3:F or B2:D100`},
		{map[string]any{"path": "a.xlsx", "range": "C3:A"}, `is not a range`},
		{map[string]any{"path": "a.xlsx", "emptyRows": "keep"}, `invalid emptyRows "keep" (expected skip or stop)`},
		{map[string]any{"path": "a.xlsx", "fields": []any{map[string]any{"name": "a"}, map[string]any{"name": "a"}}}, `the field "a" is listed twice`},
		{map[string]any{"path": "a.xlsx", "fields": []any{map[string]any{"name": "a", "type": "money"}}}, `field "a": `},
	} {
		if _, err := newInput(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
}

func TestParseRange(t *testing.T) {
	for in, want := range map[string]area{"": {}, "A3": {1, 3, 0, 0}, "a3:f": {1, 3, 6, 0}, "B2:D100": {2, 2, 4, 100}, "A:F": {1, 0, 6, 0}, " $C$5 ": {3, 5, 0, 0}} {
		got, err := parseRange(in)
		if err != nil || got != want {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
}
