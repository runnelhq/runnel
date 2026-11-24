// Package format converts values between text and the types of records (ADR 0007)
// with number patterns like #.##0,00 and date patterns like dd.MM.yyyy. csv.input,
// csv.output and core.convert share it.
package format

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/record"
)

// Codec converts text into a value of a type and back, with a pattern: a number pattern
// like #.##0,00 or a date pattern like dd.MM.yyyy (ADR 0007).
type Codec struct {
	typ    record.Type
	format string
	num    numberFormat
}

// New returns the codec for a type (empty: string) and a pattern. A pattern is only
// possible for numbers and dates.
func New(typ record.Type, pattern string) (Codec, error) {
	if typ == record.Unknown {
		typ = record.String
	}
	c := Codec{typ: typ, format: pattern}
	switch typ {
	case record.String, record.JSON:
		if pattern != "" {
			return c, errors.New("a format is only possible for numbers and dates")
		}
	case record.Int, record.Decimal, record.Float:
		n, err := parseNumberFormat(pattern)
		if err != nil {
			return c, err
		}
		c.num = n
	case record.Bool:
		if pattern != "" {
			return c, errors.New("a format is only possible for numbers and dates")
		}
	case record.DateType, record.DateTime:
		if pattern != "" {
			// Check the pattern once: formatting a fixed date fails for invalid patterns.
			if _, err := expr.FormatDate(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), pattern); err != nil {
				return c, err
			}
		}
	default:
		return c, fmt.Errorf("invalid type %q (expected %s)", typ, TypeNames())
	}
	return c, nil
}

// ForText returns the codec for writing values of any type as text with a pattern: a
// pattern of only # 0 . , ' and space is a number pattern, any other a date pattern.
func ForText(pattern string) (Codec, error) {
	if IsNumberPattern(pattern) {
		return New(record.Decimal, pattern)
	}
	return New(record.DateTime, pattern)
}

// ToText returns the codec that converts values of any type to text with a pattern
// (number or date, see ForText): Convert writes them, Parse keeps text as it is.
func ToText(pattern string) (Codec, error) {
	c, err := ForText(pattern)
	c.typ = record.String
	return c, err
}

// Type returns the type the codec reads.
func (c Codec) Type() record.Type { return c.typ }

// TypeNames lists the types a codec supports (all but binary), for messages.
func TypeNames() string {
	var names []string
	for _, t := range record.Types {
		if t != record.Binary {
			names = append(names, string(t))
		}
	}
	return strings.Join(names, ", ")
}

// Parse reads a text. An empty text is null, except for text.
func (c Codec) Parse(s string) (any, error) {
	if s == "" && c.typ != record.String {
		return nil, nil
	}
	switch c.typ {
	case record.String:
		return s, nil
	case record.Int:
		v, err := strconv.ParseInt(c.num.plain(s), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a whole number%s", s, c.num.hint())
		}
		return v, nil
	case record.Decimal:
		v, err := decimal.NewFromString(c.num.plain(s))
		if err != nil {
			return nil, fmt.Errorf("%q is not a number%s", s, c.num.hint())
		}
		return v, nil
	case record.Float:
		v, err := strconv.ParseFloat(c.num.plain(s), 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number%s", s, c.num.hint())
		}
		return v, nil
	case record.Bool:
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true", "1", "yes":
			return true, nil
		case "false", "0", "no":
			return false, nil
		}
		return nil, fmt.Errorf("%q is not true or false (also 1/0, yes/no)", s)
	case record.DateType, record.DateTime:
		t, err := c.parseTime(s)
		if err != nil {
			return nil, err
		}
		if c.typ == record.DateType {
			return record.DateOf(t), nil
		}
		return t, nil
	case record.JSON:
		var v any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return nil, fmt.Errorf("%q is not JSON", s)
		}
		return record.Normalize(v)
	}
	return s, nil
}

func (c Codec) parseTime(s string) (time.Time, error) {
	if c.format != "" {
		return expr.ParseDate(s, c.format)
	}
	if c.typ == record.DateType {
		d, err := record.ParseDate(s)
		return d.In(time.Local), err
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02T15:04:05", time.DateOnly} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is not a date and time (expected e.g. 2026-11-20T14:30:00Z; set a format)", s)
}

// Format writes a value of any type as text, with the pattern if there is one; without,
// numbers as 1234.5, dates in ISO 8601 and JSON as JSON. Null is an empty text.
func (c Codec) Format(v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case int64:
		return c.num.format(decimal.NewFromInt(v), false), nil
	case decimal.Decimal:
		return c.num.format(v, true), nil
	case float64:
		if c.num.isSet() {
			return c.num.format(decimal.NewFromFloat(v), true), nil
		}
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(v), nil
	case record.Date:
		if c.format != "" {
			return expr.FormatDate(v.In(time.Local), c.format)
		}
		return v.String(), nil
	case time.Time:
		if c.format != "" {
			return expr.FormatDate(v, c.format)
		}
		return v.Format(time.RFC3339Nano), nil
	case []byte:
		return "", errors.New("binary values cannot be written as text")
	case map[string]any, []any:
		b, err := json.Marshal(v)
		return string(b), err
	}
	return fmt.Sprint(v), nil
}

// numberFormat is a pattern like #.##0,00: the separator before the last digits is the
// decimal separator, the other one groups thousands; the zeros after the decimal
// separator are the decimals written.
type numberFormat struct {
	set      bool
	decimal  string
	group    string
	decimals int
}

func parseNumberFormat(p string) (numberFormat, error) {
	if p == "" {
		return numberFormat{decimal: "."}, nil
	}
	for _, r := range p {
		if !strings.ContainsRune("#0.,' ", r) {
			return numberFormat{}, fmt.Errorf("invalid number format %q (use # and 0 with . , ' or space, e.g. #.##0,00)", p)
		}
	}
	n := numberFormat{set: true, decimal: "."}
	dot, comma := strings.LastIndex(p, "."), strings.LastIndex(p, ",")
	dec := -1 // position of the decimal separator, if any
	switch {
	case dot >= 0 && comma >= 0:
		dec = max(dot, comma) // #.##0,00 or #,##0.00: the later one
	case dot >= 0 || comma >= 0:
		i := max(dot, comma)
		sep := p[i : i+1]
		after := p[i+1:]
		grouping := strings.Count(p, sep) > 1 || (len(after) == 3 && strings.Contains(p[:i], "#"))
		if !grouping {
			dec = i // 0,00 or 0.000
		}
	}
	intPart := p
	if dec >= 0 {
		n.decimal = p[dec : dec+1]
		n.decimals = strings.Count(p[dec+1:], "0")
		intPart = p[:dec]
	}
	for _, sep := range []string{".", ",", "'", " "} {
		if strings.Contains(intPart, sep) {
			if (dec >= 0 && sep == n.decimal) || (n.group != "" && n.group != sep) {
				return numberFormat{}, fmt.Errorf("invalid number format %q: use one separator for thousands and another one for decimals", p)
			}
			n.group = sep
		}
	}
	if dec < 0 && n.group == "." {
		n.decimal = "," // #.##0: whole numbers grouped with "." (German style)
	}
	return n, nil
}

func (n numberFormat) isSet() bool { return n.set }

// plain turns a formatted number into the form Go reads: no grouping, "." for decimals.
func (n numberFormat) plain(s string) string {
	s = strings.TrimSpace(s)
	if n.group != "" {
		s = strings.ReplaceAll(s, n.group, "")
	}
	if n.decimal != "." {
		s = strings.ReplaceAll(s, n.decimal, ".")
	}
	return s
}

func (n numberFormat) hint() string {
	if !n.set {
		return " (expected e.g. 1234.5; set a format like #.##0,00 for other separators)"
	}
	return ""
}

// format writes a number with the pattern; without a pattern exactly as it is.
func (n numberFormat) format(d decimal.Decimal, fraction bool) string {
	if !n.set {
		return d.String()
	}
	s := d.String()
	if fraction || n.decimals > 0 {
		s = d.StringFixed(int32(n.decimals)) //nolint:gosec // decimals come from a format pattern (a few digits)
	}
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	intPart, frac, _ := strings.Cut(s, ".")
	if n.group != "" {
		var b strings.Builder
		for i, r := range intPart {
			if i > 0 && (len(intPart)-i)%3 == 0 {
				b.WriteString(n.group)
			}
			b.WriteRune(r)
		}
		intPart = b.String()
	}
	if frac != "" {
		return sign + intPart + n.decimal + frac
	}
	return sign + intPart
}

// IsNumberPattern reports whether a pattern is a number pattern (only # 0 . , ' and
// space) rather than a date pattern, as ForText decides.
func IsNumberPattern(pattern string) bool {
	return pattern != "" && strings.Trim(pattern, "#0.,' ") == ""
}

// NumberPattern reads a number pattern like #.##0,00: whether it groups thousands and
// how many decimals it writes (for excel.output, which shows them in the language of
// the user).
func NumberPattern(pattern string) (grouped bool, decimals int, err error) {
	n, err := parseNumberFormat(pattern)
	return n.group != "", n.decimals, err
}
