package record

import (
	"fmt"
	"time"
)

// Date is a day without time of day and time zone (value type "date", ADR 0007). It
// stays a date when passed on, e.g. a CSV output writes 2026-11-20, and it does not
// move across midnight when time zones differ.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// DateOf returns the day of t in t's time zone.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{y, m, d}
}

// ParseDate reads a date in ISO 8601 form, e.g. 2026-11-20.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return Date{}, fmt.Errorf("%q is not a date (expected YYYY-MM-DD)", s)
	}
	return DateOf(t), nil
}

// String returns the date in ISO 8601 form, e.g. 2026-11-20.
func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

// In returns the start of the day in a time zone.
func (d Date) In(loc *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, loc)
}

// Before reports whether d is earlier than e.
func (d Date) Before(e Date) bool {
	return d.In(time.UTC).Before(e.In(time.UTC))
}

// MarshalText writes the date as 2026-11-20 (also for JSON).
func (d Date) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalText reads a date written by MarshalText.
func (d *Date) UnmarshalText(b []byte) error {
	v, err := ParseDate(string(b))
	if err != nil {
		return err
	}
	*d = v
	return nil
}
