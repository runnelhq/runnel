// Package csv contains the transforms of the namespace "csv" (roadmap S15): csv.input
// reads a CSV file, csv.output writes one. Without a field definition all values are
// text; with fields: they are converted with type and format (ADR 0007). Each transform
// lives in a file of its own; this file holds what both share.
package csv

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

// field is a column of a field definition (fields:).
type field struct {
	Name string `json:"name" doc:"Name of the field (and of the column in the header)" schema:"required,example=amount"`
	Type string `json:"type" doc:"Type of the value; empty: text" schema:"enum=string|int|decimal|float|bool|date|datetime|json"`
	// Format is a number pattern like #.##0,00 or a date pattern like dd.MM.yyyy.
	Format string `json:"format" doc:"Number pattern like #.##0,00 or date pattern like dd.MM.yyyy; empty: 1234.5 and ISO dates" schema:"example=dd.MM.yyyy"`
}

// codec converts the text of a column into a value of the field's type and back.
type codec struct {
	typ    record.Type
	format string
	num    numberFormat
}

func newCodec(f field) (codec, error) {
	t := record.Type(f.Type)
	if f.Type == "" {
		t = record.String
	}
	c := codec{typ: t, format: f.Format}
	switch t {
	case record.String, record.JSON:
		if f.Format != "" {
			return c, fmt.Errorf("field %q: a format is only possible for numbers and dates", f.Name)
		}
	case record.Int, record.Decimal, record.Float:
		n, err := parseNumberFormat(f.Format)
		if err != nil {
			return c, fmt.Errorf("field %q: %w", f.Name, err)
		}
		c.num = n
	case record.Bool:
	case record.DateType, record.DateTime:
		if f.Format != "" {
			// Check the pattern once: formatting a fixed date fails for invalid patterns.
			if _, err := expr.FormatDate(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), f.Format); err != nil {
				return c, fmt.Errorf("field %q: %w", f.Name, err)
			}
		}
	default:
		return c, fmt.Errorf("field %q: invalid type %q (expected %s)", f.Name, f.Type, typeNames())
	}
	return c, nil
}

func typeNames() string {
	var names []string
	for _, t := range record.Types {
		if t != record.Binary {
			names = append(names, string(t))
		}
	}
	return strings.Join(names, ", ")
}

// parse reads the text of a column. An empty text is null, except for text fields.
func (c codec) parse(s string) (any, error) {
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

func (c codec) parseTime(s string) (time.Time, error) {
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

// formatValue writes a value as the text of a column, with the format of the field if
// it has one; values without a field definition are written in their default form.
func (c codec) formatValue(v any) (string, error) {
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
		return "", errors.New("binary values cannot be written to CSV")
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
		s = d.StringFixed(int32(n.decimals))
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
