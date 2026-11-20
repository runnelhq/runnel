package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

func TestConstant(t *testing.T) {
	started := time.Date(2026, 11, 20, 14, 30, 0, 0, time.UTC)
	values := map[string]any{"vars": map[string]any{}, "env": map[string]any{},
		"params": map[string]any{"BATCH": int64(7)}, "run": map[string]any{"id": "R1", "startedAt": started}}
	tr := transformtest.New(t, constantDefinition, map[string]any{"fields": []any{
		map[string]any{"name": "source", "value": "import"},
		map[string]any{"name": "batch", "value": "{{ params.BATCH * 10 }}"},
		map[string]any{"name": "importedAt", "value": "{{ run.startedAt }}"},
		map[string]any{"name": "note", "value": "run {{ run.id }}"},
		map[string]any{"name": "active", "value": true},
		map[string]any{"name": "city", "value": nil},
	}})
	out, _, err := transformtest.RunWithValues(context.Background(), tr, values, customer(), customer())
	if err != nil || len(out) != 2 {
		t.Fatalf("%d records, %v", len(out), err)
	}
	// An existing field keeps its position and gets the value.
	want := `{kdnr: 4711, name: "Müller GmbH", city: null, _rowNumber: 1, source: "import", batch: 70, importedAt: 2026-11-20T14:30:00Z, note: "run R1", active: true}`
	for _, r := range out {
		if got := Format(r); got != want {
			t.Errorf("got  %s\nwant %s", got, want)
		}
	}
	if v, _ := out[0].Get("batch"); v != int64(70) {
		t.Errorf("batch %#v, want int64", v)
	}

	s, err := tr.(transform.Initializer).Init(transform.NewInitContext(record.Schema{}.With("city", record.String)))
	if err != nil {
		t.Fatal(err)
	}
	wantSchema := []record.FieldDef{{Name: "city", Type: record.Unknown}, {Name: "source", Type: record.String}, {Name: "batch"},
		{Name: "importedAt"}, {Name: "note"}, {Name: "active", Type: record.Bool}}
	if len(s.Fields) != len(wantSchema) {
		t.Fatalf("schema %+v", s)
	}
	for i, f := range wantSchema {
		if s.Fields[i] != f {
			t.Errorf("schema field %d: %+v, want %+v", i, s.Fields[i], f)
		}
	}
}

func TestConstantErrors(t *testing.T) {
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"fields": []any{}}, "fields: list at least one field"},
		{map[string]any{"fields": []any{map[string]any{"name": "a", "value": "{{ record.x }}"}}}, "fields[0] (a): expression {{ record.x }}: unknown name record"},
		{map[string]any{"fields": []any{map[string]any{"name": "a", "value": "{{ 1 + }}"}}}, "fields[0] (a): expression"},
	} {
		if _, err := constantDefinition.New(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
	// A variable in an expression is resolved before the run; until then it is checked only.
	if _, err := constantDefinition.New(map[string]any{"fields": []any{map[string]any{"name": "a", "value": "{{ ${N} * 2 }}"}}}); err != nil {
		t.Errorf("with a variable: %v", err)
	}
	tr := transformtest.New(t, constantDefinition, map[string]any{"fields": []any{map[string]any{"name": "a", "value": "{{ int('x') }}"}}})
	if _, _, err := transformtest.Run(context.Background(), tr, customer()); err == nil || !strings.Contains(err.Error(), "fields[0] (a)") {
		t.Errorf("evaluation: %v", err)
	}
}
