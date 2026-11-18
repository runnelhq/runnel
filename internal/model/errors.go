// Package model contains the file formats of Runnel (workflows, and later pipelines,
// triggers, …) as Go types, together with loading from YAML and field-level validation.
package model

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Position is a 1-based line/column position in a source file.
// The zero value means "unknown".
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

func (p Position) String() string {
	if p.Line == 0 {
		return ""
	}
	return fmt.Sprintf("%d:%d", p.Line, p.Column)
}

// Severity distinguishes errors (the file cannot be used) from warnings
// (probably a mistake, but the file can be used).
type Severity int

// Severities.
const (
	SeverityError Severity = iota
	SeverityWarning
)

func (s Severity) String() string {
	if s == SeverityWarning {
		return "warning"
	}
	return "error"
}

// Error is a problem found in a file, with its location.
type Error struct {
	File     string
	Pos      Position
	Severity Severity
	Msg      string
}

func (e *Error) Error() string {
	var b strings.Builder
	if e.File != "" {
		b.WriteString(e.File)
		b.WriteString(":")
	}
	if e.Pos.Line != 0 {
		b.WriteString(e.Pos.String())
		b.WriteString(":")
	}
	if b.Len() > 0 {
		b.WriteString(" ")
	}
	if e.Severity == SeverityWarning {
		b.WriteString("warning: ")
	}
	b.WriteString(e.Msg)
	return b.String()
}

// Errors is a list of problems in a file, sorted by position.
type Errors []*Error

// HasErrors reports whether the list contains at least one problem of severity error.
func (es Errors) HasErrors() bool {
	for _, e := range es {
		if e.Severity == SeverityError {
			return true
		}
	}
	return false
}

func (es Errors) Error() string {
	msgs := make([]string, len(es))
	for i, e := range es {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "\n")
}

func (es Errors) sort() {
	slices.SortStableFunc(es, func(a, b *Error) int {
		return cmp.Or(cmp.Compare(a.Pos.Line, b.Pos.Line), cmp.Compare(a.Pos.Column, b.Pos.Column))
	})
}

// errOrNil returns nil for an empty list, so callers can use "if err != nil".
func (es Errors) errOrNil() error {
	if len(es) == 0 {
		return nil
	}
	es.sort()
	return es
}
