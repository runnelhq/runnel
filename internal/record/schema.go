package record

import "slices"

// FieldDef is a field of a schema: its name and, if known, its type.
type FieldDef struct {
	Name string `json:"name"`
	Type Type   `json:"type,omitempty"`
}

// Schema describes the records on a link (ADR 0007): the known fields in their order
// and whether further fields may occur. A closed schema lists all fields; an open one
// lists those known so far (e.g. after a JSON input or a script).
type Schema struct {
	Fields []FieldDef `json:"fields"`
	Open   bool       `json:"open,omitempty"`
}

// OpenSchema is a schema about which nothing is known.
func OpenSchema() Schema { return Schema{Open: true} }

// Field returns the definition of a field and whether the schema knows it.
func (s Schema) Field(name string) (FieldDef, bool) {
	for _, f := range s.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return FieldDef{}, false
}

// Names returns the names of the known fields in their order.
func (s Schema) Names() []string {
	names := make([]string, len(s.Fields))
	for i, f := range s.Fields {
		names[i] = f.Name
	}
	return names
}

// With returns a copy of the schema with a field added at the end or, if it exists,
// its type changed.
func (s Schema) With(name string, t Type) Schema {
	out := Schema{Fields: slices.Clone(s.Fields), Open: s.Open}
	for i, f := range out.Fields {
		if f.Name == name {
			out.Fields[i].Type = t
			return out
		}
	}
	out.Fields = append(out.Fields, FieldDef{Name: name, Type: t})
	return out
}

// Merge combines the schemas of several links into one transform (records of all of
// them arrive): fields in order of first appearance; a field with different types, or
// missing in one of the schemas, has an unknown type; the result is open if one of
// them is open. Without schemas, the result is an empty closed schema.
func Merge(schemas ...Schema) Schema {
	if len(schemas) == 1 {
		return schemas[0]
	}
	var out Schema
	for _, s := range schemas {
		out.Open = out.Open || s.Open
		for _, f := range s.Fields {
			if _, ok := out.Field(f.Name); !ok {
				out.Fields = append(out.Fields, f)
			}
		}
	}
	for i, f := range out.Fields {
		for _, s := range schemas {
			g, ok := s.Field(f.Name)
			if !ok || g.Type != f.Type {
				out.Fields[i].Type = Unknown
			}
		}
	}
	return out
}
