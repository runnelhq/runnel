package format

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/runnelhq/runnel/internal/record"
)

// Proposal is the type and format proposed for the values of a field (S46).
type Proposal struct {
	Type   record.Type
	Format string
	// Note explains the proposal when the values are kept as text although many of them
	// fit a type; Warning marks notes about values that do not fit.
	Note    string
	Warning bool
}

// candidates are the types and formats Propose tries, in this order: the first that
// every value fits wins.
// A grouped number must also have the shape of one (shape), as reading it only drops
// the separators: 19.11.2026 is no number.
var candidates = []struct {
	typ    record.Type
	format string
	shape  *regexp.Regexp
}{
	{record.Int, "", nil},
	{record.Decimal, "", nil},
	{record.Decimal, "#.##0,00", regexp.MustCompile(`^[+-]?(\d{1,3}(\.\d{3})*|\d+)(,\d+)?$`)},
	{record.Decimal, "#,##0.00", regexp.MustCompile(`^[+-]?(\d{1,3}(,\d{3})*|\d+)(\.\d+)?$`)},
	{record.DateType, "", nil},
	{record.DateType, "dd.MM.yyyy", nil},
	{record.DateTime, "", nil},
	{record.DateTime, "dd.MM.yyyy HH:mm:ss", nil},
	{record.DateTime, "dd.MM.yyyy HH:mm", nil},
	{record.Bool, "", nil},
}

// Propose proposes a type and format for text values (e.g. a column of a CSV file):
// a type only if every value that is not empty fits it, else text. lines are the line
// numbers of the values for notes (may be nil). Numbers with a leading zero (0221 …)
// stay text, as the zero would be lost.
func Propose(values []string, lines []int) Proposal {
	var given []int // indexes of the values that are not empty
	leadingZero := false
	for i, v := range values {
		if strings.TrimSpace(v) == "" {
			continue
		}
		given = append(given, i)
		if hasLeadingZero(v) {
			leadingZero = true
		}
	}
	if len(given) == 0 {
		return Proposal{Type: record.String, Note: "No values: kept as text"}
	}
	best, bestFit, bestFailed := -1, 0, []int(nil)
	for ci, c := range candidates {
		codec, err := New(c.typ, c.format)
		if err != nil {
			continue
		}
		var failed []int
		many := false // more than half do not fit: neither the type nor a note
		for _, i := range given {
			v := strings.TrimSpace(values[i])
			if _, err := codec.Parse(v); err != nil || (c.shape != nil && !c.shape.MatchString(v)) {
				failed = append(failed, i)
				if many = len(failed)*2 > len(given); many {
					break
				}
			}
		}
		if many {
			continue
		}
		numeric := c.typ == record.Int || c.typ == record.Decimal
		if len(failed) == 0 {
			if numeric && leadingZero {
				return Proposal{Type: record.String, Note: "Leading zero: kept as text"}
			}
			return Proposal{Type: c.typ, Format: c.format}
		}
		if fit := len(given) - len(failed); fit > bestFit && (!numeric || !leadingZero) {
			best, bestFit, bestFailed = ci, fit, failed
		}
	}
	if best >= 0 {
		c := candidates[best]
		what := string(c.typ)
		if c.format != "" {
			what += " (" + c.format + ")"
		}
		at := ""
		if lines != nil {
			at = fmt.Sprintf(" in line %d", lines[bestFailed[0]])
		}
		first := fmt.Sprintf("“%s”%s", values[bestFailed[0]], at)
		verb := "is"
		if len(bestFailed) > 1 {
			first += fmt.Sprintf(" and %d more", len(bestFailed)-1)
			verb = "are"
		}
		return Proposal{Type: record.String, Warning: true,
			Note: fmt.Sprintf("%d of %d values are %s; %s %s not. Kept as text.", bestFit, len(given), what, first, verb)}
	}
	if leadingZero && digitsOnly(values, given) {
		return Proposal{Type: record.String, Note: "Leading zero: kept as text"}
	}
	if yesNo(values, given) {
		return Proposal{Type: record.String, Note: "ja/nein is not a bool (true/false, 1/0, yes/no): kept as text"}
	}
	return Proposal{Type: record.String}
}

// hasLeadingZero reports a number written with a leading zero, like 0221 or 007.
func hasLeadingZero(v string) bool {
	v = strings.TrimLeft(strings.TrimSpace(v), "+-")
	return len(v) > 1 && v[0] == '0' && v[1] >= '0' && v[1] <= '9'
}

// digits are values made of digits and the separators of phone numbers and codes.
var digits = regexp.MustCompile(`^\+?[0-9][0-9 /().-]*$`)

// digitsOnly reports values that look like numbers written with a leading zero kept on
// purpose, e.g. phone numbers (0221 123456) or postcodes.
func digitsOnly(values []string, given []int) bool {
	for _, i := range given {
		if !digits.MatchString(strings.TrimSpace(values[i])) {
			return false
		}
	}
	return true
}

// yesNo reports German yes/no values, which bool does not read.
func yesNo(values []string, given []int) bool {
	for _, i := range given {
		switch strings.ToLower(strings.TrimSpace(values[i])) {
		case "ja", "nein":
		default:
			return false
		}
	}
	return true
}
