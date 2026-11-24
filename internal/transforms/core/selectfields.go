package core

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// core.select-fields keeps the listed fields (in this order, optionally renamed) or
// removes the listed fields.
var selectFieldsDefinition = transform.Definition{
	Parallel:    true,
	Type:        "core.select-fields",
	Category:    transform.CategoryTransform,
	Label:       "Select Fields",
	Description: "Keeps, orders and renames fields, or removes fields",
	Example:     "fields:\n  - { name: kdnr, as: externalId }\n  - { name: name }\n# or instead of fields:\n# remove: [telefon]\n",
	Config:      selectFields{},
	New:         newSelectFields,
}

type selectFields struct {
	Fields []selectField `json:"fields" doc:"Fields to keep, in this order, optionally renamed; all others are removed"`
	Remove []string      `json:"remove" doc:"Fields to remove; all others are kept. Use either fields or remove" schema:"format=runnel-field"`
}

type selectField struct {
	Name string `json:"name" doc:"Name of the field in the incoming records" schema:"required,format=runnel-field,example=kdnr"`
	As   string `json:"as" doc:"New name of the field; empty: the name stays" schema:"example=customerId"`
}

func newSelectFields(with map[string]any) (transform.Transform, error) {
	t := &selectFields{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	if len(t.Fields) > 0 && len(t.Remove) > 0 {
		return nil, errors.New("use either fields (to keep) or remove, not both")
	}
	var names []string
	for i, f := range t.Fields {
		if f.Name == "" {
			return nil, fmt.Errorf("fields[%d]: every field needs a name", i)
		}
		if slices.Contains(names, f.out()) {
			return nil, fmt.Errorf("fields[%d]: the field %q would exist twice", i, f.out())
		}
		names = append(names, f.out())
	}
	for i, name := range t.Remove {
		if name == "" {
			return nil, fmt.Errorf("remove[%d]: the name is empty", i)
		}
	}
	return t, nil
}

// out is the name of the field in the outgoing records.
func (f selectField) out() string {
	if f.As != "" {
		return f.As
	}
	return f.Name
}

// Init checks the fields and reports the schema: closed with the kept fields (their types
// from the incoming schema), or the incoming one without the removed fields.
func (t *selectFields) Init(ic *transform.InitContext) (record.Schema, error) {
	if len(t.Fields) == 0 && len(t.Remove) == 0 {
		ic.Warn("", "nothing is selected: list the fields to keep (fields) or to remove (remove); the records are passed on unchanged")
		return ic.In, nil
	}
	if len(t.Remove) > 0 {
		out := record.Schema{Open: ic.In.Open}
		for i, name := range t.Remove {
			ic.Use(name, fmt.Sprintf("remove[%d]", i))
		}
		for _, f := range ic.In.Fields {
			if !slices.Contains(t.Remove, f.Name) {
				out.Fields = append(out.Fields, f)
			}
		}
		return out, nil
	}
	var out record.Schema
	for i, f := range t.Fields {
		ic.Use(f.Name, fmt.Sprintf("fields[%d].name", i))
		def, _ := ic.In.Field(f.Name)
		out = out.With(f.out(), def.Type)
	}
	return out, nil
}

func (t *selectFields) Process(_ context.Context, in <-chan transform.Batch, out *transform.Output) error {
	for b := range in {
		for _, r := range b {
			switch {
			case len(t.Remove) > 0:
				for _, name := range t.Remove {
					r.Delete(name)
				}
			case len(t.Fields) > 0:
				kept := record.Make(len(t.Fields))
				for _, f := range t.Fields {
					v, _ := r.Get(f.Name) // a missing field is null
					kept.Set(f.out(), v)
				}
				r = kept
			}
			if err := out.Emit(r); err != nil {
				return err
			}
		}
	}
	return nil
}
