// Package record defines the records that flow through pipelines (ADR 0007): an
// ordered list of fields, name → value. The order is kept (important for CSV and
// Excel). Field names starting with "_" are reserved for metadata (_error, _source,
// _rowNumber). Values are plain Go values for now; the value types of ADR 0007 and
// schemas follow with roadmap S14 without changing this type's methods.
package record

// Field is a name and its value.
type Field struct {
	Name  string
	Value any
}

// Record is an ordered list of fields with unique names. The zero value is an empty
// record. Records are not safe for concurrent use; the engine gives each transform
// its own copy where several transforms receive the same record.
type Record struct {
	fields []Field
}

// New creates a record from fields; a later field with the same name replaces an earlier one.
func New(fields ...Field) Record {
	var r Record
	for _, f := range fields {
		r.Set(f.Name, f.Value)
	}
	return r
}

// Make returns an empty record with room for n fields, so that adding them with Set
// does not grow it step by step.
func Make(n int) Record { return Record{fields: make([]Field, 0, n)} }

// FromFields returns a record of the fields, which must have unique names (e.g. the
// columns of a file, checked once). The slice is taken over and must not be changed
// afterwards. Unlike New, it does not look for duplicates, which costs time for every
// field of every record.
func FromFields(fields []Field) Record { return Record{fields: fields} }

// Len returns the number of fields.
func (r Record) Len() int { return len(r.fields) }

// Fields returns the fields in their order. The slice must not be changed.
func (r Record) Fields() []Field { return r.fields }

// Names returns the field names in their order.
func (r Record) Names() []string {
	names := make([]string, len(r.fields))
	for i, f := range r.fields {
		names[i] = f.Name
	}
	return names
}

func (r Record) index(name string) int {
	for i, f := range r.fields {
		if f.Name == name {
			return i
		}
	}
	return -1
}

// Get returns the value of a field and whether the field exists.
func (r Record) Get(name string) (any, bool) {
	if i := r.index(name); i >= 0 {
		return r.fields[i].Value, true
	}
	return nil, false
}

// GetAt is Get for a field that is usually at position i, e.g. a column of a file whose
// records all have the same fields in the same order: it looks there first.
func (r Record) GetAt(i int, name string) (any, bool) {
	if i >= 0 && i < len(r.fields) && r.fields[i].Name == name {
		return r.fields[i].Value, true
	}
	return r.Get(name)
}

// Set changes the value of a field; a new field is added at the end.
func (r *Record) Set(name string, value any) {
	if i := r.index(name); i >= 0 {
		r.fields[i].Value = value
		return
	}
	r.fields = append(r.fields, Field{Name: name, Value: value})
}

// Delete removes a field; the order of the others stays.
func (r *Record) Delete(name string) {
	if i := r.index(name); i >= 0 {
		r.fields = append(r.fields[:i:i], r.fields[i+1:]...)
	}
}

// Clone returns a copy whose fields can be changed independently. Values themselves
// are not copied (e.g. nested maps are shared).
func (r Record) Clone() Record {
	if r.fields == nil {
		return Record{}
	}
	return Record{fields: append([]Field(nil), r.fields...)}
}

// Map returns the record as map, e.g. for expressions or JSON; the order is lost.
func (r Record) Map() map[string]any {
	m := make(map[string]any, len(r.fields))
	for _, f := range r.fields {
		m[f.Name] = f.Value
	}
	return m
}
