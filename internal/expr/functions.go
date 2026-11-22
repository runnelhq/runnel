package expr

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	exprlang "github.com/expr-lang/expr"
)

// The function library, part 1 (ADR 0006): dates, text, null and empty values. The
// built-in functions of expr-lang (upper, lower, trim, split, replace, len, now, …)
// are available as well. The type declarations let compiling find wrong arguments.
var functions = []exprlang.Option{
	exprlang.Function("today", func(...any) (any, error) { return today(), nil }, new(func() time.Time)),
	dateFunc("addDays", func(t time.Time, n int) any { return t.AddDate(0, 0, n) }),
	dateFunc("addHours", func(t time.Time, n int) any { return t.Add(time.Duration(n) * time.Hour) }),
	dateFunc("addMonths", func(t time.Time, n int) any { return t.AddDate(0, n, 0) }),
	exprlang.Function("formatDate", func(p ...any) (any, error) {
		t, err := toTime(p[0])
		if err != nil {
			return nil, err
		}
		return FormatDate(t, toString(p[1]))
	}, new(func(time.Time, string) string), new(func(string, string) string)),
	exprlang.Function("parseDate", func(p ...any) (any, error) { return ParseDate(toString(p[0]), toString(p[1])) },
		new(func(string, string) time.Time)),
	exprlang.Function("dayOfWeek", func(p ...any) (any, error) {
		t, err := toTime(p[0])
		if err != nil {
			return nil, err
		}
		return isoWeekday(t), nil
	}, new(func(time.Time) int), new(func(string) int)),
	exprlang.Function("diffDays", func(p ...any) (any, error) {
		a, err := toTime(p[0])
		if err != nil {
			return nil, err
		}
		b, err := toTime(p[1])
		if err != nil {
			return nil, err
		}
		return diffDays(a, b), nil
	}),

	exprlang.Function("padLeft", func(p ...any) (any, error) { return pad(p, true) },
		new(func(string, int) string), new(func(string, int, string) string)),
	exprlang.Function("padRight", func(p ...any) (any, error) { return pad(p, false) },
		new(func(string, int) string), new(func(string, int, string) string)),
	exprlang.Function("substr", func(p ...any) (any, error) {
		r := []rune(toString(p[0]))
		start, err := toInt(p[1])
		if err != nil {
			return nil, err
		}
		start = clamp(start, len(r))
		end := len(r)
		if len(p) > 2 {
			n, err := toInt(p[2])
			if err != nil {
				return nil, err
			}
			end = clamp(start+max(n, 0), len(r))
		}
		return string(r[start:end]), nil
	}, new(func(string, int) string), new(func(string, int, int) string)),
	exprlang.Function("left", func(p ...any) (any, error) {
		r := []rune(toString(p[0]))
		n, err := toInt(p[1])
		if err != nil {
			return nil, err
		}
		return string(r[:clamp(n, len(r))]), nil
	}, new(func(string, int) string)),
	exprlang.Function("right", func(p ...any) (any, error) {
		r := []rune(toString(p[0]))
		n, err := toInt(p[1])
		if err != nil {
			return nil, err
		}
		return string(r[len(r)-clamp(n, len(r)):]), nil
	}, new(func(string, int) string)),
	exprlang.Function("normalize", func(p ...any) (any, error) { return normalize(toString(p[0])), nil },
		new(func(string) string)),

	exprlang.Function("concat", concat, new(func(string, ...any) string)),

	exprlang.Function("coalesce", func(p ...any) (any, error) {
		for _, v := range p {
			if !isEmpty(v) {
				return v, nil
			}
		}
		return nil, nil
	}),
	exprlang.Function("isEmpty", func(p ...any) (any, error) {
		if len(p) != 1 {
			return nil, fmt.Errorf("isEmpty expects 1 argument, got %d", len(p))
		}
		return isEmpty(p[0]), nil
	}),
}

// concat joins the values after the separator as text and leaves out empty ones (nothing,
// empty text or only spaces), so that no separator stands alone: concat(', ', ”, 'Köln')
// is "Köln". Lists count with their entries; numbers and dates are written as by Format.
func concat(p ...any) (any, error) {
	if len(p) == 0 {
		return nil, fmt.Errorf("concat expects a separator and values, e.g. concat(', ', a, b)")
	}
	sep, ok := p[0].(string)
	if !ok {
		return nil, fmt.Errorf("concat: the first argument is the separator (a text), got %s", Format(p[0]))
	}
	var parts []string
	var add func(v any)
	add = func(v any) {
		if l, ok := v.([]any); ok {
			for _, e := range l {
				add(e)
			}
			return
		}
		if !isEmpty(v) {
			parts = append(parts, Format(v))
		}
	}
	for _, v := range p[1:] {
		add(v)
	}
	return strings.Join(parts, sep), nil
}

// dateFunc declares f(date, number) for a date given as date or text.
func dateFunc(name string, f func(time.Time, int) any) exprlang.Option {
	return exprlang.Function(name, func(p ...any) (any, error) {
		t, err := toTime(p[0])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		n, err := toInt(p[1])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		return f(t, n), nil
	}, new(func(any, any) time.Time))
}

// toTime accepts a date or a text in ISO 8601 ("2026-11-19", "2026-11-19T14:30:00Z", "2026-11-19 14:30:00").
func toTime(v any) (time.Time, error) {
	switch v := v.(type) {
	case time.Time:
		return v, nil
	case string:
		for _, l := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
			if t, err := time.ParseInLocation(l, v, time.Local); err == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("%q is not a date (expected e.g. 2026-11-19 or 2026-11-19T14:30:00)", v)
	}
	return time.Time{}, fmt.Errorf("expected a date, got %s", Format(v))
}

// toInt accepts whole numbers of any type, also as float64 (numbers from YAML or JSON).
func toInt(v any) (int, error) {
	switch v := v.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case int32:
		return int(v), nil
	case float64:
		if v == float64(int(v)) {
			return int(v), nil
		}
	}
	return 0, fmt.Errorf("expected a whole number, got %s", Format(v))
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return Format(v)
}

func today() time.Time {
	y, m, d := time.Now().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

// isoWeekday returns 1 for Monday … 7 for Sunday.
func isoWeekday(t time.Time) int {
	if wd := t.Weekday(); wd != time.Sunday {
		return int(wd)
	}
	return 7
}

// diffDays counts the calendar days from a to b (negative if b is earlier).
func diffDays(a, b time.Time) int {
	day := func(t time.Time) time.Time {
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	return int(day(b).Sub(day(a)).Hours() / 24)
}

func pad(p []any, left bool) (any, error) {
	s := toString(p[0])
	n, err := toInt(p[1])
	if err != nil {
		return nil, err
	}
	with := " "
	if len(p) > 2 {
		with = toString(p[2])
	}
	if with == "" {
		return nil, fmt.Errorf("the padding text must not be empty")
	}
	missing := n - utf8.RuneCountInString(s)
	if missing <= 0 {
		return s, nil
	}
	fill := []rune(strings.Repeat(with, missing/utf8.RuneCountInString(with)+1))[:missing]
	if left {
		return string(fill) + s, nil
	}
	return s + string(fill), nil
}

func clamp(i, n int) int {
	return min(max(i, 0), n)
}

var umlauts = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "Ä", "Ae", "Ö", "Oe", "Ü", "Ue", "ß", "ss")

// normalize replaces German umlauts and ß, and collapses white space.
func normalize(s string) string {
	s = umlauts.Replace(s)
	return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
}

// isEmpty is true for nil, texts of only white space and empty lists or maps.
func isEmpty(v any) bool {
	switch v := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Map, reflect.Array:
		return rv.Len() == 0
	case reflect.Pointer, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

// datePatterns are the pattern letters of formatDate and parseDate (as in Hop, Java and
// .NET) and their Go layouts; longer patterns first.
var datePatterns = []struct{ pattern, layout string }{
	{"yyyy", "2006"}, {"yy", "06"},
	{"MMMM", "January"}, {"MMM", "Jan"}, {"MM", "01"}, {"M", "1"},
	{"dd", "02"}, {"d", "2"},
	{"EEEE", "Monday"}, {"EEE", "Mon"},
	{"HH", "15"}, {"hh", "03"}, {"h", "3"},
	{"mm", "04"}, {"m", "4"},
	{"ss", "05"}, {"s", "5"},
	{"SSS", "000"},
	{"a", "PM"},
	{"XXX", "Z07:00"}, {"Z", "-0700"},
}

type dateToken struct {
	literal bool
	text    string // the literal text, or the Go layout of a pattern letter group
}

// tokens splits a pattern like "dd.MM.yyyy 'at' HH:mm" into pattern letters (as Go
// layouts) and literal text. Text in single quotes is literal (” is a quote); letters
// outside quotes must be pattern letters, digits must be quoted.
func tokens(pattern string) ([]dateToken, error) {
	var out []dateToken
	for i := 0; i < len(pattern); {
		c := pattern[i]
		switch {
		case c == '\'':
			end := strings.IndexByte(pattern[i+1:], '\'')
			if end < 0 {
				return nil, fmt.Errorf("date pattern %q: missing closing quote", pattern)
			}
			lit := pattern[i+1 : i+1+end]
			if end == 0 {
				lit = "'"
			}
			out = append(out, dateToken{literal: true, text: lit})
			i += end + 2
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			found := false
			for _, p := range datePatterns {
				if strings.HasPrefix(pattern[i:], p.pattern) {
					out = append(out, dateToken{text: p.layout})
					i += len(p.pattern)
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("date pattern %q: unknown letter %q (use yyyy, MM, dd, HH, mm, ss …; text in 'quotes')", pattern, c)
			}
		case c >= '0' && c <= '9':
			return nil, fmt.Errorf("date pattern %q: digits must be in 'quotes'", pattern)
		default:
			out = append(out, dateToken{literal: true, text: string(c)})
			i++
		}
	}
	return out, nil
}

// FormatDate formats t with a pattern like "yyyy-MM-dd".
func FormatDate(t time.Time, pattern string) (string, error) {
	toks, err := tokens(pattern)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, tok := range toks {
		if tok.literal {
			b.WriteString(tok.text)
		} else {
			b.WriteString(t.Format(tok.text))
		}
	}
	return b.String(), nil
}

// goLayoutWords are parts of Go layouts that literal text must not contain when parsing.
var goLayoutWords = []string{"Jan", "Mon", "MST", "PM", "pm", "_"}

// ParseDate reads s with a pattern like "dd.MM.yyyy"; without a time zone in the
// pattern, the local time zone applies.
func ParseDate(s, pattern string) (time.Time, error) {
	toks, err := tokens(pattern)
	if err != nil {
		return time.Time{}, err
	}
	var b strings.Builder
	for _, tok := range toks {
		if tok.literal {
			if strings.ContainsAny(tok.text, "0123456789") || slices.ContainsFunc(goLayoutWords, func(w string) bool { return strings.Contains(tok.text, w) }) {
				return time.Time{}, fmt.Errorf("date pattern %q: the text %q cannot be read back", pattern, tok.text)
			}
		}
		b.WriteString(tok.text)
	}
	t, err := time.ParseInLocation(b.String(), s, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("parseDate: %q does not match the pattern %q", s, pattern)
	}
	return t, nil
}
