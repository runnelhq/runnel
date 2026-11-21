package transform

import (
	"fmt"
	"strings"
)

// DetectLimit is the number of rows DetectFields reads at most (S46).
const DetectLimit = 1000

// Detection is what an input transform found out about its fields from the first rows
// of its input ("Detect fields" in the editor, S46, ADR 0007). It is a proposal: the
// types are written to "fields:" and never guessed when a pipeline runs.
type Detection struct {
	// Source describes what was read, e.g. the file, the delimiter and the encoding.
	Source []Fact `json:"source"`
	// Rows is the number of rows read; Complete is set when they are all of the input.
	Rows     int  `json:"rows"`
	Complete bool `json:"complete"`
	// Fields are the proposed fields, in the order to write them.
	Fields []DetectedField `json:"fields"`
	// ByPosition: the fields of the configuration are matched by position (e.g. a CSV
	// file without header), not by name.
	ByPosition bool `json:"-"`
}

// Fact is a line of the description of the input, e.g. Encoding: UTF-8.
type Fact struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// DetectedField is a proposed field.
type DetectedField struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Format string `json:"format,omitempty"`
	// Values are the first values read, as text ("" for an empty value).
	Values []string `json:"values"`
	// Note explains the proposal, e.g. why a column is kept as text; Warning marks a
	// note the user should look at (some values do not fit a type).
	Note    string `json:"note,omitempty"`
	Warning bool   `json:"warning,omitempty"`
	// State compares the field with the fields of the configuration: empty without
	// fields, else listed (in both), new (only in the input) or missing (only listed).
	State string `json:"state,omitempty"`
	// Selected proposes to write the field: all without fields; with fields the listed
	// ones, as the list also selects what is read.
	Selected bool `json:"selected"`
}

// States of a DetectedField.
const (
	FieldListed  = "listed"
	FieldNew     = "new"
	FieldMissing = "missing"
)

// MergeFields compares a detection with the fields of the configuration ("fields:" with
// name, type and format): listed fields keep their type and format and stay selected,
// new ones are not selected, listed ones the input does not have are added as missing.
// The order is that of the configuration, then the new fields in the order of the input.
func MergeFields(d *Detection, configured any) error {
	listed, err := fieldList(configured)
	if err != nil || len(listed) == 0 {
		for i := range d.Fields {
			d.Fields[i].Selected = true
		}
		return err
	}
	found := map[int]bool{}
	var out []DetectedField
	for i, l := range listed {
		j := -1
		for k, f := range d.Fields {
			if !found[k] && ((d.ByPosition && k == i) || (!d.ByPosition && f.Name == l.Name)) {
				j = k
				break
			}
		}
		f := DetectedField{Name: l.Name, Type: l.Type, Format: l.Format, State: FieldMissing, Selected: true,
			Values: []string{}, Note: "Not in the input"}
		if f.Type == "" {
			f.Type = "string"
		}
		if j >= 0 {
			found[j] = true
			f.State, f.Values, f.Note = FieldListed, d.Fields[j].Values, ""
		}
		out = append(out, f)
	}
	for k, f := range d.Fields {
		if !found[k] {
			f.State, f.Selected = FieldNew, false
			out = append(out, f)
		}
	}
	d.Fields = out
	return nil
}

// field is an entry of "fields:" as MergeFields reads it.
type field struct{ Name, Type, Format string }

func fieldList(v any) ([]field, error) {
	if v == nil {
		return nil, nil
	}
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("fields must be a list")
	}
	out := make([]field, 0, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("fields[%d] must be a mapping with name, type and format", i)
		}
		f := field{}
		for key, dst := range map[string]*string{"name": &f.Name, "type": &f.Type, "format": &f.Format} {
			if s, ok := m[key].(string); ok {
				*dst = strings.TrimSpace(s)
			}
		}
		out = append(out, f)
	}
	return out, nil
}
