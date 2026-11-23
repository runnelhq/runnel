package model

import "slices"

// fileFormats are the file formats with a JSON Schema (roadmap S58), by kind in lower
// case: the structs that files of the kind are read into. The schema package derives
// the schemas from their yaml names and doc tags (ADR 0014).
var fileFormats = map[string]any{
	"project":     Project{},
	"environment": Environment{},
	"trigger":     Trigger{},
	"connection":  Connection{},
}

// FileFormat returns the struct of a file kind (project, environment, trigger, connection) for its
// schema; false if the kind has none.
func FileFormat(kind string) (any, bool) {
	f, ok := fileFormats[kind]
	return f, ok
}

// FileFormats returns the kinds with a schema, sorted.
func FileFormats() []string {
	var out []string
	for k := range fileFormats {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
