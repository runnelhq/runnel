package vars

import (
	"cmp"
	"slices"
	"strings"
	"sync"
)

// Masked replaces the value of a secret in logs, messages and results.
const Masked = "***"

// minMaskLength is the shortest value that is masked: shorter ones would replace single
// characters and short words everywhere (ADR 0022).
const minMaskLength = 4

// Mask hides the values of the secrets of a run (ADR 0022): every value read with
// ExpandSecret is added, and texts, errors and results are passed through it before
// they are shown. It finds the exact value only, not an encoded one (e.g. Base64). The
// zero value and a nil *Mask are ready to use; a Mask is safe for concurrent use.
type Mask struct {
	mu     sync.RWMutex
	values []string // longest first, so that a value containing another is replaced whole
}

// Add registers the value of a secret; values under 4 characters are not masked.
func (m *Mask) Add(value string) {
	if m == nil || len(value) < minMaskLength {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if slices.Contains(m.values, value) {
		return
	}
	m.values = append(m.values, value)
	slices.SortFunc(m.values, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
}

// Lookup returns lookup with every secret it reads added to the mask.
func (m *Mask) Lookup(lookup func(store, name string) (string, error)) func(store, name string) (string, error) {
	if m == nil || lookup == nil {
		return lookup
	}
	return func(store, name string) (string, error) {
		v, err := lookup(store, name)
		if err == nil {
			m.Add(v)
		}
		return v, err
	}
}

// String returns s with the values of the secrets replaced by ***.
func (m *Mask) String(s string) string {
	if m == nil || s == "" {
		return s
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, v := range m.values {
		s = strings.ReplaceAll(s, v, Masked)
	}
	return s
}

// Error returns err with a masked message; errors.Is and errors.As still see err.
func (m *Mask) Error(err error) error {
	if err == nil {
		return nil
	}
	if msg := m.String(err.Error()); msg != err.Error() {
		return &maskedError{msg: msg, err: err}
	}
	return err
}

type maskedError struct {
	msg string
	err error
}

func (e *maskedError) Error() string { return e.msg }
func (e *maskedError) Unwrap() error { return e.err }

// Value returns v with the secrets masked in all texts, also inside maps and lists
// (e.g. the outputs of a workflow). v itself is not changed.
func (m *Mask) Value(v any) any {
	if m == nil {
		return v
	}
	m.mu.RLock()
	empty := len(m.values) == 0
	m.mu.RUnlock()
	if empty {
		return v
	}
	switch v := v.(type) {
	case string:
		return m.String(v)
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[k] = m.Value(e)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = m.Value(e)
		}
		return out
	}
	return v
}

// Map is Value for a map, e.g. outputs.
func (m *Mask) Map(v map[string]any) map[string]any {
	if v == nil {
		return nil
	}
	out, _ := m.Value(v).(map[string]any)
	return out
}
