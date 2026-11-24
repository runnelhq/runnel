package xlsx

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
)

// kind is what a number format shows: a number, a date, a date with time, or a time.
type kind uint8

const (
	kindNumber kind = iota
	kindDate
	kindDateTime
	kindTime
)

// builtin returns the kind of the built-in number formats of Excel (ECMA-376, 18.8.30),
// including the East Asian ones.
func builtin(id int) kind {
	switch {
	case id >= 14 && id <= 17, id >= 27 && id <= 31, id >= 34 && id <= 36, id >= 50 && id <= 58:
		return kindDate
	case id == 22:
		return kindDateTime
	case id >= 18 && id <= 21, id >= 32 && id <= 33, id >= 45 && id <= 47:
		return kindTime
	}
	return kindNumber
}

// kindOfCode decides from a format code like dd.mm.yyyy or [h]:mm whether it shows a
// date or time. Only the first section counts (the one for positive numbers); texts in
// quotes, escaped characters, colours and conditions do not.
func kindOfCode(code string) kind {
	var sb strings.Builder
	quoted := false
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case quoted:
			quoted = c != '"'
		case c == '"':
			quoted = true
		case c == '\\' || c == '_' || c == '*':
			i++ // the next character is shown as it is or fills space
		case c == ';':
			i = len(code)
		case c == '[':
			end := strings.IndexByte(code[i:], ']')
			if end < 0 {
				i = len(code)
				break
			}
			inner := strings.ToLower(code[i+1 : i+end])
			if strings.Trim(inner, "hms") == "" && inner != "" {
				sb.WriteString(inner) // elapsed time: [h], [mm], [ss]
			}
			i += end
		default:
			sb.WriteByte(c)
		}
	}
	s := strings.ToLower(sb.String())
	if strings.Contains(s, "general") {
		return kindNumber
	}
	date := strings.ContainsAny(s, "yd")
	clock := strings.ContainsAny(s, "hs")
	switch {
	case date && clock:
		return kindDateTime
	case date:
		return kindDate
	case clock:
		return kindTime
	case strings.Contains(s, "m") && !strings.ContainsAny(s, "0#?"):
		return kindDate // a month alone, e.g. mmmm
	}
	return kindNumber
}

// serial converts a serial number of Excel into a date or time: the whole days count
// from 1899-12-30 (1904-01-01 in the 1904 system), the fraction is the time of day.
// Excel counts the 29th of February 1900, which did not exist: days before it are one
// later. Times are wall-clock times without a time zone, read in the local one.
func serial(n decimal.Decimal, k kind, date1904 bool) (any, error) {
	f := n.InexactFloat64()
	if f < 0 || f > 2958465 { // 9999-12-31
		return nil, fmt.Errorf("%s is not a date in Excel", n)
	}
	if k == kindTime {
		return clock(f), nil
	}
	days := math.Floor(f)
	ms := math.Round((f - days) * 86400000)
	if ms >= 86400000 {
		days, ms = days+1, 0
	}
	base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	switch {
	case date1904:
		base = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
	case days < 60:
		base = base.AddDate(0, 0, 1)
	case days == 60:
		return nil, fmt.Errorf("%s is the 29th of February 1900, which Excel shows but which did not exist", n)
	}
	day := base.AddDate(0, 0, int(days))
	if k == kindDate && ms == 0 {
		return record.DateOf(day), nil
	}
	// A date with a time, also in a date format: keep the time.
	y, m, d := day.Date()
	t := time.Duration(ms) * time.Millisecond
	return time.Date(y, m, d, int(t/time.Hour), int(t/time.Minute%60), int(t/time.Second%60), int(t%time.Second), time.Local), nil
}

// clock writes a time of day or a duration ([h]:mm) as text, e.g. 08:30:00 or 25:00:00:
// Runnel has no type for times without a date.
func clock(f float64) string {
	s := int64(math.Round(f * 86400))
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, s/60%60, s%60)
}

// Date converts a number of a cell without date format into a date (or a date and time,
// with time set), for fields of type date: e.g. a column whose dates Excel shows as
// numbers.
func (b *Book) Date(n decimal.Decimal, time bool) (any, error) {
	k := kindDate
	if time {
		k = kindDateTime
	}
	return serial(n, k, b.date1904)
}
