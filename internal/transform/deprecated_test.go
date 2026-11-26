package transform

import (
	"context"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/action"
)

type nothing struct{}

func (nothing) Process(context.Context, <-chan Batch, *Output) error { return nil }

// A renamed field of a transform (ADR 0029; test definition, nothing is deprecated yet)
// reaches New and DetectFields under its new name and is marked in the schema.
func TestDeprecatedField(t *testing.T) {
	var got []map[string]any
	keep := func(with map[string]any) { got = append(got, with) }
	r := NewRegistry()
	r.Register(Definition{Type: "test.out", Category: CategoryOutput, Label: "Out", Description: "Test",
		Config: struct {
			MaxRecords int `json:"maxRecords" doc:"Most records"`
		}{},
		Deprecated: []action.Deprecated{{Field: "maxRows", RenamedTo: "maxRecords", RemovedIn: "2.0"}},
		New:        func(with map[string]any) (Transform, error) { keep(with); return nothing{}, nil },
		DetectFields: func(_ context.Context, with map[string]any, _ int) (*Detection, error) {
			keep(with)
			return &Detection{}, nil
		}})
	def, _ := r.Lookup("test.out")
	if _, err := def.New(map[string]any{"maxRows": 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := def.DetectFields(t.Context(), map[string]any{"maxRows": 5}, 10); err != nil {
		t.Fatal(err)
	}
	for _, with := range got {
		if len(with) != 1 || with["maxRecords"] != 5 {
			t.Errorf("mapped to %v", with)
		}
	}
	if _, err := def.New(map[string]any{"maxRows": 5, "maxRecords": 6}); err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Errorf("both: %v", err)
	}
	if _, err := def.DetectFields(t.Context(), map[string]any{"maxRows": 5, "maxRecords": 6}, 10); err == nil {
		t.Error("both: no error from DetectFields")
	}
	d, _ := r.Descriptor("test.out")
	if p, ok := d.Schema.Properties.Lookup("maxRows"); !ok || !p.Deprecated || p.Replacement != "use maxRecords (removed in 2.0)" || p.Type != "integer" {
		t.Errorf("schema %+v", p)
	}
}
