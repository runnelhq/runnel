package core

import (
	"context"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

func customer() record.Record {
	return record.New(
		record.Field{Name: "kdnr", Value: int64(4711)},
		record.Field{Name: "name", Value: "Müller GmbH"},
		record.Field{Name: "city", Value: "Köln"},
		record.Field{Name: "_rowNumber", Value: int64(1)},
	)
}

func selectOne(t *testing.T, with map[string]any) string {
	t.Helper()
	tr := transformtest.New(t, selectFieldsDefinition, with)
	out, _, err := transformtest.Run(context.Background(), tr, customer())
	if err != nil || len(out) != 1 {
		t.Fatalf("%v: %d records, %v", with, len(out), err)
	}
	return Format(out[0])
}

func TestSelectFields(t *testing.T) {
	// Keep in this order, rename; a missing field is null.
	got := selectOne(t, map[string]any{"fields": []any{
		map[string]any{"name": "name", "rename": "companyName"},
		map[string]any{"name": "kdnr", "rename": "externalId"},
		map[string]any{"name": "phone"},
	}})
	if want := `{companyName: "Müller GmbH", externalId: 4711, phone: null}`; got != want {
		t.Errorf("keep: %s, want %s", got, want)
	}
	// Remove: the others stay in their order.
	if got, want := selectOne(t, map[string]any{"remove": []any{"city", "_rowNumber"}}), `{kdnr: 4711, name: "Müller GmbH"}`; got != want {
		t.Errorf("remove: %s, want %s", got, want)
	}
	// Nothing configured: unchanged (with a warning from Init, see below).
	if got := selectOne(t, nil); !strings.HasPrefix(got, "{kdnr: 4711") {
		t.Errorf("nothing: %s", got)
	}
}

func TestSelectFieldsConfigErrors(t *testing.T) {
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"fields": []any{map[string]any{"name": "a"}}, "remove": []any{"b"}}, "use either fields (to keep) or remove, not both"},
		{map[string]any{"fields": []any{map[string]any{"name": "a", "rename": "b"}, map[string]any{"name": "b"}}}, `fields[1]: the field "b" would exist twice`},
		{map[string]any{"fields": []any{map[string]any{"rename": "b"}}}, `missing required field "fields[0].name"`},
		{map[string]any{"remove": []any{""}}, "remove[0]: the name is empty"},
	} {
		if _, err := selectFieldsDefinition.New(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
}

func TestSelectFieldsSchema(t *testing.T) {
	in := record.Schema{}.With("kdnr", record.Int).With("name", record.String).With("city", record.String)
	init := func(with map[string]any, in record.Schema) (record.Schema, []transform.Problem) {
		tr := transformtest.New(t, selectFieldsDefinition, with)
		ic := transform.NewInitContext(in)
		s, err := tr.(transform.Initializer).Init(ic)
		if err != nil {
			t.Fatal(err)
		}
		return s, ic.Problems()
	}
	s, p := init(map[string]any{"fields": []any{map[string]any{"name": "name", "rename": "companyName"}, map[string]any{"name": "kdnr"}}}, in)
	if s.Open || len(p) != 0 || len(s.Fields) != 2 || s.Fields[0] != (record.FieldDef{Name: "companyName", Type: record.String}) || s.Fields[1] != (record.FieldDef{Name: "kdnr", Type: record.Int}) {
		t.Errorf("keep: %+v %+v", s, p)
	}
	s, p = init(map[string]any{"remove": []any{"city"}}, in)
	if len(p) != 0 || strings.Join(s.Names(), ",") != "kdnr,name" {
		t.Errorf("remove: %+v %+v", s, p)
	}
	_, p = init(map[string]any{"fields": []any{map[string]any{"name": "nam"}}}, in)
	if len(p) != 1 || p[0].Path != "fields[0].name" || !strings.Contains(p[0].Message, `did you mean "name"?`) {
		t.Errorf("typo: %+v", p)
	}
	_, p = init(map[string]any{"remove": []any{"cty"}}, in)
	if len(p) != 1 || p[0].Path != "remove[0]" {
		t.Errorf("typo in remove: %+v", p)
	}
	s, p = init(nil, in)
	if len(p) != 1 || !p[0].Warning || !strings.Contains(p[0].Message, "nothing is selected") || len(s.Fields) != 3 {
		t.Errorf("nothing: %+v %+v", s, p)
	}
}
