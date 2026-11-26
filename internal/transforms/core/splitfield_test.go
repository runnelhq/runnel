package core

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

func split(t *testing.T, with map[string]any, in ...record.Record) []string {
	t.Helper()
	out, _, err := transformtest.Run(context.Background(), transformtest.New(t, splitFieldDefinition, with), in...)
	if err != nil {
		t.Fatalf("%v: %v", with, err)
	}
	var got []string
	for _, r := range out {
		got = append(got, fmt.Sprint(r.Fields()))
	}
	return got
}

func person(id int64, name any) record.Record {
	return record.New(record.Field{Name: "id", Value: id}, record.Field{Name: "name", Value: name})
}

func TestSplitFieldInto(t *testing.T) {
	got := split(t, map[string]any{"field": "name", "separator": ", ", "into": []any{"last", "first"}},
		person(1, "Weber, Anna"), person(2, "Weber, Anna, Dr."), person(3, "Müller GmbH"), person(4, nil))
	want := []string{
		"[{id 1} {name Weber, Anna} {last Weber} {first Anna}]",
		// More parts than fields: the rest stays in the last one.
		"[{id 2} {name Weber, Anna, Dr.} {last Weber} {first Anna, Dr.}]",
		"[{id 3} {name Müller GmbH} {last Müller GmbH} {first <nil>}]",
		"[{id 4} {name <nil>} {last <nil>} {first <nil>}]",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// trim, and into the field itself.
	got = split(t, map[string]any{"field": "name", "separator": ",", "into": []any{"name", "rest"}, "trim": true}, person(1, "Weber ,  Anna "))
	if fmt.Sprint(got) != "[[{id 1} {name Weber} {rest Anna}]]" {
		t.Errorf("trim: %v", got)
	}
}

func TestSplitFieldRows(t *testing.T) {
	got := split(t, map[string]any{"field": "name", "separator": ";", "asRecords": true, "numberField": "pos"},
		person(1, "a;b;;c"), person(2, ""), person(3, 7))
	want := []string{
		"[{id 1} {name a} {pos 1}]", "[{id 1} {name b} {pos 2}]", "[{id 1} {name c} {pos 3}]",
		// An empty field is passed on once; a number is split as text.
		"[{id 2} {name <nil>} {pos <nil>}]",
		"[{id 3} {name 7} {pos 1}]",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	got = split(t, map[string]any{"field": "name", "separator": ";", "asRecords": true, "keepEmpty": true}, person(1, "a;;b"))
	if fmt.Sprint(got) != "[[{id 1} {name a}] [{id 1} {name }] [{id 1} {name b}]]" {
		t.Errorf("keepEmpty: %v", got)
	}
}

func TestSplitFieldErrors(t *testing.T) {
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"field": "n", "separator": ";"}, "set asRecords: true"},
		// rows and indexField are asRecords and numberField since R5b.
		{map[string]any{"field": "n", "separator": ";", "rows": true}, `field "rows" was renamed before 1.0: use asRecords`},
		{map[string]any{"field": "n", "separator": ";", "asRecords": true, "indexField": "i"}, `field "indexField" was renamed before 1.0: use numberField`},
		{map[string]any{"field": "n", "separator": ";", "asRecords": true, "into": []any{"a"}}, "choose one"},
		{map[string]any{"field": "n", "separator": ";", "into": []any{"a"}, "numberField": "i"}, "only with asRecords"},
		{map[string]any{"field": "n", "separator": "", "asRecords": true}, "separator"},
	} {
		if _, err := newSplitField(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
}

func TestSplitFieldSchema(t *testing.T) {
	tr := transformtest.New(t, splitFieldDefinition, map[string]any{"field": "name", "separator": ";", "asRecords": true, "numberField": "pos"})
	ic := transform.NewInitContext(record.Schema{}.With("name", record.String))
	s, err := tr.(transform.Initializer).Init(ic)
	if f, ok := s.Field("pos"); err != nil || !ok || f.Type != record.Int {
		t.Errorf("schema %v %v", s.Names(), err)
	}
	tr = transformtest.New(t, splitFieldDefinition, map[string]any{"field": "naem", "separator": ";", "into": []any{"a", "b"}})
	ic = transform.NewInitContext(record.Schema{}.With("name", record.String))
	s, _ = tr.(transform.Initializer).Init(ic)
	if strings.Join(s.Names(), ",") != "name,a,b" {
		t.Errorf("names %v", s.Names())
	}
	if p := ic.Problems(); len(p) != 1 || !strings.Contains(p[0].Message, `did you mean "name"?`) {
		t.Errorf("problems %+v", p)
	}
}

func BenchmarkSplitFieldRows(b *testing.B) {
	tr, err := newSplitField(map[string]any{"field": "name", "separator": ";", "asRecords": true})
	if err != nil {
		b.Fatal(err)
	}
	in := make([]record.Record, 1000)
	b.ReportAllocs()
	for b.Loop() {
		for i := range in {
			in[i] = person(int64(i), "b2b;premium;nrw")
		}
		if _, _, err := transformtest.Run(context.Background(), tr, in...); err != nil {
			b.Fatal(err)
		}
	}
}
