// Package vars implements variable references in texts ("${NAME}", "${env:NAME}",
// "${secret:NAME}") and their resolution over layered scopes.
//
// Syntax:
//
//	${NAME}         variable from the scope (project, environment, parameters, …)
//	${env:NAME}     environment variable of the operating system
//	${secret:NAME}  secret from the secret store
//	$${             literal "${" (escape)
//
// Variable values may reference other variables; they are expanded recursively.
package vars

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
)

// Kind is the namespace of a reference.
type Kind string

// Reference kinds.
const (
	KindVar    Kind = ""
	KindEnv    Kind = "env"
	KindSecret Kind = "secret"
)

// Ref is a reference "${…}" found in a text.
type Ref struct {
	Kind Kind
	Name string
	// Offset is the byte offset of "${" in the text.
	Offset int
}

func (r Ref) String() string {
	if r.Kind == KindVar {
		return "${" + r.Name + "}"
	}
	return "${" + string(r.Kind) + ":" + r.Name + "}"
}

var (
	varNamePattern    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	secretNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

// ValidName reports whether name is a valid variable name: letters, digits and "_",
// not starting with a digit.
func ValidName(name string) bool {
	return varNamePattern.MatchString(name)
}

// Parse returns all references in text. Escaped "$${" is skipped.
func Parse(text string) ([]Ref, error) {
	var refs []Ref
	err := scan(text, func(r Ref) {
		refs = append(refs, r)
	}, nil)
	return refs, err
}

// scan walks text, calls onRef for every reference and onText for every literal part
// (with escapes already resolved). Either callback may be nil.
func scan(text string, onRef func(Ref), onText func(string)) error {
	emit := func(s string) {
		if onText != nil && s != "" {
			onText(s)
		}
	}
	for i := 0; i < len(text); {
		j := strings.Index(text[i:], "${")
		if j < 0 {
			emit(text[i:])
			return nil
		}
		start := i + j
		if start > 0 && text[start-1] == '$' {
			// "$${" is an escaped "${": keep "${" literally.
			emit(text[i:start-1] + "${")
			i = start + 2
			continue
		}
		emit(text[i:start])
		end := strings.IndexByte(text[start:], '}')
		if end < 0 {
			return fmt.Errorf("unclosed %q at position %d", "${", start+1)
		}
		ref, err := parseRef(text[start+2:start+end], start)
		if err != nil {
			return err
		}
		if onRef != nil {
			onRef(ref)
		}
		i = start + end + 1
	}
	return nil
}

func parseRef(body string, offset int) (Ref, error) {
	kind, name := KindVar, body
	if k, n, ok := strings.Cut(body, ":"); ok {
		kind, name = Kind(k), n
	}
	switch kind {
	case KindVar:
		if !ValidName(name) {
			return Ref{}, fmt.Errorf("invalid variable name %q in %q (letters, digits and _, not starting with a digit)", name, "${"+body+"}")
		}
	case KindEnv:
		if name == "" || strings.ContainsAny(name, "${=") {
			return Ref{}, fmt.Errorf("invalid environment variable name %q in %q", name, "${"+body+"}")
		}
	case KindSecret:
		if !secretNamePattern.MatchString(name) {
			return Ref{}, fmt.Errorf("invalid secret name %q in %q (letters, digits, ., - and _)", name, "${"+body+"}")
		}
	default:
		return Ref{}, fmt.Errorf("unknown reference type %q in %q (expected ${NAME}, ${env:NAME} or ${secret:NAME})", kind, "${"+body+"}")
	}
	return Ref{Kind: kind, Name: name, Offset: offset}, nil
}

// Layer is a named set of variables, e.g. the variables of the project or of an environment.
type Layer struct {
	Name   string
	Values map[string]string
}

// Scope resolves variables over layers; later layers override earlier ones.
type Scope struct {
	layers []Layer
	// LookupEnv reads environment variables; defaults to os.LookupEnv.
	LookupEnv func(name string) (string, bool)
	// LookupSecret reads secrets; nil means secrets are not available.
	LookupSecret func(name string) (string, error)
}

// NewScope creates a scope from layers, ordered from lowest to highest priority.
func NewScope(layers ...Layer) *Scope {
	return &Scope{layers: layers, LookupEnv: os.LookupEnv}
}

// Lookup returns the raw (unexpanded) value of a variable and the name of the layer it comes from.
func (s *Scope) Lookup(name string) (value, layer string, ok bool) {
	for i := len(s.layers) - 1; i >= 0; i-- {
		if v, ok := s.layers[i].Values[name]; ok {
			return v, s.layers[i].Name, true
		}
	}
	return "", "", false
}

// Has reports whether a variable is defined in any layer.
func (s *Scope) Has(name string) bool {
	_, _, ok := s.Lookup(name)
	return ok
}

// ErrUndefined is returned (wrapped) by Expand for references to undefined variables.
var ErrUndefined = errors.New("undefined")

// Expand replaces all references in text. Variable values are expanded recursively.
func (s *Scope) Expand(text string) (string, error) {
	return s.expand(text, nil)
}

func (s *Scope) expand(text string, chain []string) (string, error) {
	var b strings.Builder
	var firstErr error
	err := scan(text, func(r Ref) {
		if firstErr != nil {
			return
		}
		v, err := s.resolve(r, chain)
		if err != nil {
			firstErr = err
			return
		}
		b.WriteString(v)
	}, func(lit string) {
		b.WriteString(lit)
	})
	if err == nil {
		err = firstErr
	}
	if err != nil {
		return "", err
	}
	return b.String(), nil
}

func (s *Scope) resolve(r Ref, chain []string) (string, error) {
	switch r.Kind {
	case KindEnv:
		lookup := s.LookupEnv
		if lookup == nil {
			lookup = os.LookupEnv
		}
		v, ok := lookup(r.Name)
		if !ok {
			return "", fmt.Errorf("environment variable %q is %w", r.Name, ErrUndefined)
		}
		return v, nil
	case KindSecret:
		if s.LookupSecret == nil {
			return "", fmt.Errorf("secret %q: secrets are not available", r.Name)
		}
		return s.LookupSecret(r.Name)
	}
	for _, c := range chain {
		if c == r.Name {
			return "", fmt.Errorf("variable cycle: %s → %s", strings.Join(chain, " → "), r.Name)
		}
	}
	v, _, ok := s.Lookup(r.Name)
	if !ok {
		return "", fmt.Errorf("variable %q is %w", r.Name, ErrUndefined)
	}
	return s.expand(v, append(chain, r.Name))
}

// Names returns the names of all variables of all layers, sorted.
func (s *Scope) Names() []string {
	seen := map[string]bool{}
	for _, l := range s.layers {
		for k := range l.Values {
			seen[k] = true
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// ReplaceLayer returns a copy of the scope in which the values of the layer with the
// given name are replaced (e.g. parameter defaults computed at the start of a run).
func (s *Scope) ReplaceLayer(name string, values map[string]string) *Scope {
	c := *s
	c.layers = slices.Clone(s.layers)
	for i, l := range c.layers {
		if l.Name == name {
			c.layers[i].Values = values
		}
	}
	return &c
}
