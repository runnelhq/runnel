package action

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/schema"
)

// Deprecated describes a field of a configuration that is renamed or deprecated
// (ADR 0029, section 4). A renamed field is accepted under both names for the whole
// major version: the old one is mapped to the new one before New, "runnel validate" and
// the editor warn about it, and both at once are an error. The catalog marks it with
// "deprecated" and the replacement. Transforms use the same type.
type Deprecated struct {
	// Field is the path of the old field in "with", e.g. "overwrite"; "fields[].rename"
	// names a field in each entry of the list fields, "auth.user" one in a mapping.
	Field string
	// RenamedTo is the field in the same mapping that takes the value, e.g. "ifExists".
	// Empty for a field that is only deprecated; it then stays in Config and New gets it
	// unchanged.
	RenamedTo string
	// Convert turns the old value into the new one, e.g. overwrite: true into "replace";
	// nil keeps the value. Texts with ${…} or {{ … }} are passed unchanged before the run
	// (New only checks the structure then) and converted once they are resolved.
	Convert func(old any) (any, error)
	// Schema is the schema of the old field where it differs from that of RenamedTo
	// (with Convert, e.g. a boolean for a choice); nil: a copy of the new one.
	Schema *schema.Schema
	// Use is the replacement named in the warning, e.g. "ifExists: replace"; empty:
	// RenamedTo.
	Use string
	// RemovedIn is the major version without the field, e.g. "2.0".
	RemovedIn string
}

// name is the last part of Field, as written in the file.
func (d Deprecated) name() string {
	return d.Field[strings.LastIndexAny(d.Field, ".]")+1:]
}

// Replacement tells what to write instead, e.g. "use ifExists: replace (removed in 2.0)".
func (d Deprecated) Replacement() string {
	text := "use " + cmp.Or(d.Use, d.RenamedTo)
	if d.Use == "" && d.RenamedTo == "" {
		text = "do not use it any more"
	}
	if d.RemovedIn != "" {
		text += " (removed in " + d.RemovedIn + ")"
	}
	return text
}

// Message is the warning for a use of the field, e.g. field "overwrite" is deprecated:
// use ifExists: replace (removed in 2.0).
func (d Deprecated) Message() string {
	return fmt.Sprintf("field %q is deprecated: %s", d.name(), d.Replacement())
}

// steps splits Field into the keys of the mappings on its way; "[]" after a key stands
// for every entry of a list.
func (d Deprecated) steps() []string {
	return strings.Split(d.Field, ".")
}

// DeprecatedUse is a use of a deprecated field in a configuration.
type DeprecatedUse struct {
	// Path is the YAML path of the field below "with", e.g. ".overwrite" or
	// ".fields[1].rename".
	Path    string
	Message string
}

// FindDeprecated returns the uses of deprecated fields in a configuration, in the order
// of deps and of the entries of lists.
func FindDeprecated(with map[string]any, deps []Deprecated) []DeprecatedUse {
	var uses []DeprecatedUse
	for _, d := range deps {
		walkField(with, d.steps(), "", func(m map[string]any, key, path string) {
			if _, ok := m[key]; ok {
				uses = append(uses, DeprecatedUse{Path: path + "." + key, Message: d.Message()})
			}
		})
	}
	return uses
}

// MapDeprecated returns the configuration with renamed fields under their new names,
// converted if needed; with itself is not changed. An old and a new field in the same
// mapping are an error.
func MapDeprecated(with map[string]any, deps []Deprecated) (map[string]any, error) {
	for _, d := range deps {
		if d.RenamedTo == "" {
			continue
		}
		out, _, err := renameField(with, d.steps(), d)
		if err != nil {
			return nil, err
		}
		with = out
	}
	return with, nil
}

// renameField renames the field d in the mapping m along steps. Mappings and lists on
// the way are copied only where a field was renamed (changed).
func renameField(m map[string]any, steps []string, d Deprecated) (out map[string]any, changed bool, err error) {
	key, list := strings.CutSuffix(steps[0], "[]")
	v, ok := m[key]
	if !ok {
		return m, false, nil
	}
	if len(steps) == 1 {
		if _, both := m[d.RenamedTo]; both {
			return nil, false, fmt.Errorf("fields %q and %q cannot be used together; %s is deprecated, use %s",
				key, d.RenamedTo, key, d.RenamedTo)
		}
		if d.Convert != nil && !hasVariables(v) {
			if v, err = d.Convert(v); err != nil {
				return nil, false, fmt.Errorf("field %q: %w", key, err)
			}
		}
		out = maps.Clone(m)
		delete(out, key)
		out[d.RenamedTo] = v
		return out, true, nil
	}
	var next any
	switch v := v.(type) {
	case map[string]any:
		if list {
			return m, false, nil
		}
		if next, changed, err = renameField(v, steps[1:], d); err != nil {
			return nil, false, err
		}
	case []any:
		if !list {
			return m, false, nil
		}
		var items []any
		for i, item := range v {
			im, isMap := item.(map[string]any)
			if !isMap {
				continue
			}
			n, c, err := renameField(im, steps[1:], d)
			if err != nil {
				return nil, false, fmt.Errorf("%s[%d]: %w", key, i, err)
			}
			if c {
				if items == nil {
					items = slices.Clone(v)
				}
				items[i], changed = n, true
			}
		}
		next = items
	}
	if !changed {
		return m, false, nil
	}
	out = maps.Clone(m)
	out[key] = next
	return out, true, nil
}

// walkField calls fn for each mapping that may hold the last key of steps, with its path.
func walkField(m map[string]any, steps []string, path string, fn func(m map[string]any, key, path string)) {
	if len(steps) == 1 {
		fn(m, steps[0], path)
		return
	}
	key, list := strings.CutSuffix(steps[0], "[]")
	switch v := m[key].(type) {
	case map[string]any:
		if !list {
			walkField(v, steps[1:], path+"."+key, fn)
		}
	case []any:
		if list {
			for i, item := range v {
				if im, ok := item.(map[string]any); ok {
					walkField(im, steps[1:], fmt.Sprintf("%s.%s[%d]", path, key, i), fn)
				}
			}
		}
	}
}

func hasVariables(v any) bool {
	s, ok := v.(string)
	return ok && (strings.Contains(s, "${") || strings.Contains(s, "{{"))
}

// DeprecateSchema marks the deprecated fields in the schema of a configuration: a
// renamed field gets a copy of the schema of its new name (or Deprecated.Schema), placed
// after it; it gets "deprecated" and the replacement, as does a field that is only
// deprecated. It fails for a path that is not in the schema, which
// is a programming error.
func DeprecateSchema(s *schema.Schema, deps []Deprecated) error {
	for _, d := range deps {
		obj := s
		steps := d.steps()
		for _, step := range steps[:len(steps)-1] {
			key, list := strings.CutSuffix(step, "[]")
			p, ok := obj.Properties.Lookup(key)
			if !ok {
				return fmt.Errorf("deprecated field %s: no field %q", d.Field, key)
			}
			if list {
				p = p.Items
			}
			if p == nil {
				return fmt.Errorf("deprecated field %s: %q is not a list", d.Field, key)
			}
			obj = p
		}
		name := d.name()
		mark := func(p *schema.Schema) {
			p.Deprecated, p.Replacement = true, d.Replacement()
		}
		if d.RenamedTo == "" {
			p, ok := obj.Properties.Lookup(name)
			if !ok {
				return fmt.Errorf("deprecated field %s: not in the configuration", d.Field)
			}
			mark(p)
			continue
		}
		if _, ok := obj.Properties.Lookup(name); ok {
			return fmt.Errorf("renamed field %s: still in the configuration", d.Field)
		}
		i := slices.IndexFunc(obj.Properties, func(p schema.Property) bool { return p.Name == d.RenamedTo })
		if i < 0 {
			return fmt.Errorf("renamed field %s: no field %q", d.Field, d.RenamedTo)
		}
		old := *obj.Properties[i].Schema
		if d.Schema != nil {
			old = *d.Schema
		}
		mark(&old)
		obj.Properties = slices.Insert(obj.Properties, i+1, schema.Property{Name: name, Schema: &old})
	}
	return nil
}
