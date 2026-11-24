package excel

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

func summary(d *transform.Detection) string {
	var b strings.Builder
	for _, f := range d.Fields {
		fmt.Fprintf(&b, "%s %s %q %q %v | %s", f.Name, f.Type, f.Format, f.State, f.Selected, strings.Join(f.Values, ","))
		if f.Note != "" {
			fmt.Fprintf(&b, " | %s", f.Note)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestDetectFields(t *testing.T) {
	d, err := detectFields(context.Background(), map[string]any{"path": orders, "range": "A3", "emptyRows": "stop"}, transform.DetectLimit)
	if err != nil {
		t.Fatal(err)
	}
	want := `Nr int "" "" true | 1,2,3
Kunde string "" "" true | Müller,Schmidt,Ärger
Betrag decimal "" "" true | 12.5,,7 | 1 cell shows an error like #N/A; its row goes to the error link
Datum string "" "" true | 2026-11-02,2026-11-03,05.11.2026 | cells of different kinds (e.g. numbers and text); kept as text
Bezahlt bool "" "" true | true,false,true
Notiz string "" "" true | ,Rückfrage,
`
	if got := summary(d); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if d.Rows != 3 || !d.Complete || fmt.Sprint(d.Source) != "[{File testdata/orders.xlsx} {Sheet Orders} {Header row 3} {Columns A to F}]" {
		t.Errorf("rows %d complete %v source %v", d.Rows, d.Complete, d.Source)
	}

	// Limit, listed fields, a sheet without header.
	d, err = detectFields(context.Background(), map[string]any{"path": orders, "sheet": "Raw", "header": false,
		"fields": []any{map[string]any{"name": "name", "type": "string"}}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := summary(d); got != "name string \"\" \"listed\" true | a\nfield2 int \"\" \"new\" false | 1\n" || d.Complete || d.Rows != 1 {
		t.Errorf("got\n%s %v %d", got, d.Complete, d.Rows)
	}
	if _, err := detectFields(context.Background(), map[string]any{"path": orders, "sheet": "x"}, 10); err == nil {
		t.Error("no error for a missing sheet")
	}
}

func TestPropose(t *testing.T) {
	for _, tt := range []struct {
		values []any
		want   string
	}{
		{nil, "string only empty cells; kept as text"},
		{[]any{"19.11.2026", "20.11.2026"}, "date dd.MM.yyyy"},
		{[]any{int64(1), decimalOf("1.5"), nil}, "decimal "},
		{[]any{dateOf("2026-11-02"), timeOf()}, "datetime "},
		{[]any{true, "ja"}, "string cells of different kinds (e.g. numbers and text); kept as text"},
	} {
		f := propose("x", tt.values)
		if got := strings.Join(strings.Fields(f.Type+" "+f.Format+" "+f.Note), " "); got != strings.TrimSpace(tt.want) {
			t.Errorf("%v: %q, want %q", tt.values, got, tt.want)
		}
	}
}

func TestInit(t *testing.T) {
	tr, err := newInput(map[string]any{"path": orders, "fields": []any{map[string]any{"name": "Nr", "type": "int"}, map[string]any{"name": "Kunde"}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := tr.(transform.Initializer).Init(transform.NewInitContext(record.Schema{}))
	if err != nil || s.Open || fmt.Sprint(s.Names()) != "[Nr Kunde]" {
		t.Fatalf("%v %v", s, err)
	}
	if f, _ := s.Field("Nr"); f.Type != record.Int {
		t.Errorf("Nr: %v", f.Type)
	}
	if f, _ := s.Field("Kunde"); f.Type != record.Unknown {
		t.Errorf("Kunde: %v", f.Type)
	}
	tr, _ = newInput(map[string]any{"path": orders})
	if s, _ := tr.(transform.Initializer).Init(transform.NewInitContext(record.Schema{})); !s.Open {
		t.Error("without fields the schema is open")
	}
}
