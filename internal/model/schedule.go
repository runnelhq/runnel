package model

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ScheduleTimes returns the next n times of a cron schedule after the given time, in its
// time zone (empty: the computer's), for the form of a trigger (E20a).
func ScheduleTimes(spec, timezone string, after time.Time, n int) ([]time.Time, error) {
	loc := time.Local
	if timezone != "" {
		l, err := time.LoadLocation(timezone)
		if err != nil {
			return nil, fmt.Errorf("unknown time zone %q", timezone)
		}
		loc = l
	}
	s, err := cronParser.Parse(strings.TrimSpace(spec))
	if err != nil {
		return nil, fmt.Errorf("invalid schedule: %s", cronError(err))
	}
	var out []time.Time
	t := after.In(loc)
	for range n {
		if t = s.Next(t); t.IsZero() {
			break
		}
		out = append(out, t)
	}
	return out, nil
}

var weekdays = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

var months = []string{"", "January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}

var descriptors = map[string]string{
	"@yearly": "At 00:00 on 1 January", "@annually": "At 00:00 on 1 January",
	"@monthly": "At 00:00 on day 1 of the month", "@weekly": "At 00:00 on Sunday",
	"@daily": "At 00:00, every day", "@midnight": "At 00:00, every day", "@hourly": "Every hour, at minute 0",
}

// DescribeSchedule tells a cron schedule in words, e.g. "At 02:00, Monday to Friday"
// for "0 2 * * 1-5". Schedules it cannot tell plainly give "" – the form then shows the
// next times only.
func DescribeSchedule(spec string) string {
	spec = strings.TrimSpace(spec)
	if d, ok := descriptors[spec]; ok {
		return d
	}
	f := strings.Fields(spec)
	if len(f) != 5 {
		return ""
	}
	when := describeTime(f[0], f[1])
	days := describeDays(f[2], f[3], f[4])
	if when == "" || days == "" {
		return ""
	}
	return when + ", " + days
}

// describeTime tells minute and hour: "At 02:00", "At 06:00 and 18:00", "Every hour, at
// minute 15", "Every 15 minutes", "Every 15 minutes from 08:00 to 17:59".
func describeTime(minute, hour string) string {
	mins, okM := cronNumbers(minute, 0, 59)
	hours, okH := cronNumbers(hour, 0, 23)
	step, every := strings.CutPrefix(minute, "*/")
	n, err := strconv.Atoi(step)
	every = every && err == nil && n > 0 && n < 60
	switch {
	case okM && okH && len(mins)*len(hours) <= 4:
		var times []string
		for _, h := range hours {
			for _, m := range mins {
				times = append(times, fmt.Sprintf("%02d:%02d", h, m))
			}
		}
		return "At " + wordList(times)
	case okM && len(mins) == 1 && hour == "*":
		return fmt.Sprintf("Every hour, at minute %d", mins[0])
	case every && hour == "*":
		return fmt.Sprintf("Every %d minutes", n)
	case every:
		if from, to, ok := strings.Cut(hour, "-"); ok {
			a, errA := strconv.Atoi(from)
			b, errB := strconv.Atoi(to)
			if errA == nil && errB == nil && a <= b && b < 24 {
				return fmt.Sprintf("Every %d minutes from %02d:00 to %02d:59", n, a, b)
			}
		}
	}
	return ""
}

// describeDays tells day of month, month and day of week: "every day", "Monday to
// Friday", "on Monday and Thursday", "on day 1 of the month", "on day 1 in January".
func describeDays(dom, month, dow string) string {
	var parts []string
	switch {
	case dom == "*" && dow == "*":
		parts = append(parts, "every day")
	case dom == "*":
		if a, b, ok := strings.Cut(dow, "-"); ok {
			x, ok1 := weekday(a)
			y, ok2 := weekday(b)
			if !ok1 || !ok2 || x >= y {
				return ""
			}
			parts = append(parts, weekdays[x]+" to "+weekdays[y])
		} else {
			var names []string
			for p := range strings.SplitSeq(dow, ",") {
				d, ok := weekday(p)
				if !ok {
					return ""
				}
				names = append(names, weekdays[d])
			}
			parts = append(parts, "on "+wordList(names))
		}
	case dow == "*":
		days, ok := cronNumbers(dom, 1, 31)
		if !ok {
			return ""
		}
		var names []string
		for _, d := range days {
			names = append(names, strconv.Itoa(d))
		}
		parts = append(parts, "on day "+wordList(names)+" of the month")
	default:
		return "" // day of month or day of week: plain words would mislead
	}
	if month != "*" {
		ms, ok := cronNumbers(month, 1, 12)
		if !ok {
			return ""
		}
		var names []string
		for _, m := range ms {
			names = append(names, months[m])
		}
		parts = append(parts, "in "+wordList(names))
	}
	return strings.Join(parts, " ")
}

// cronNumbers reads a field of single values and a list of them ("5", "0,30"), within min
// and max; ranges and steps give false.
func cronNumbers(field string, lo, hi int) ([]int, bool) {
	var out []int
	for p := range strings.SplitSeq(field, ",") {
		n, err := strconv.Atoi(p)
		if err != nil || n < lo || n > hi {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// weekday reads a day of the week, 0–7 (0 and 7 are Sunday) or its English short name.
func weekday(s string) (int, bool) {
	if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 7 {
		return n % 7, true
	}
	for i, w := range weekdays {
		if strings.EqualFold(s, w[:3]) {
			return i, true
		}
	}
	return 0, false
}

// wordList joins words as "a", "a and b" or "a, b and c".
func wordList(words []string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}
