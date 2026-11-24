package excel

import (
	"fmt"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/format"
)

// excelCode translates a Runnel pattern (ADR 0025) into the number format code of
// Excel: a number pattern like #.##0,00 becomes #,##0.00, a date pattern like
// dd.MM.yyyy HH:mm becomes dd.mm.yyyy hh:mm. Excel shows the separators and the names
// of months and days in the language of the user. What Excel cannot show is an error.
func excelCode(pattern string) (string, error) {
	if format.IsNumberPattern(pattern) {
		grouped, decimals, err := format.NumberPattern(pattern)
		if err != nil {
			return "", err
		}
		code := "0"
		if grouped {
			code = "#,##0"
		}
		if decimals > 0 {
			code += "." + strings.Repeat("0", decimals)
		}
		return code, nil
	}
	parts, err := expr.DateParts(pattern)
	if err != nil {
		return "", fmt.Errorf("%w; for a number format of Excel like 0.0%% use excelFormat", err)
	}
	return dateCode(pattern, parts)
}

// dateLetters are the Excel codes of the date pattern letters; minutes and months are
// both m in Excel, see dateCode.
var dateLetters = map[string]string{
	"yyyy": "yyyy", "yy": "yy",
	"MMMM": "mmmm", "MMM": "mmm", "MM": "mm", "M": "m",
	"dd": "dd", "d": "d", "EEEE": "dddd", "EEE": "ddd",
	"HH": "hh", "hh": "hh", "h": "h",
	"mm": "mm", "m": "m", "ss": "ss", "s": "s",
	"SSS": "000", "a": "AM/PM",
}

func dateCode(pattern string, parts []expr.DatePart) (string, error) {
	fail := func(why string) (string, error) {
		return "", fmt.Errorf("date pattern %q: %s; set excelFormat with a number format of Excel instead", pattern, why)
	}
	has := func(letters ...string) bool {
		for _, p := range parts {
			if slices.Contains(letters, p.Letters) {
				return true
			}
		}
		return false
	}
	switch {
	case has("XXX", "Z"):
		return fail("Excel has no time zones")
	case has("HH") && has("a"):
		return fail("Excel shows HH with a as 12-hour time; use hh with a")
	case has("hh", "h") && !has("a"):
		return fail("Excel shows hh without a as 24-hour time; use HH, or add a")
	}
	var b strings.Builder
	prev := "" // letters of the previous part with letters
	for i, p := range parts {
		if p.Letters == "" {
			for _, r := range p.Text {
				if strings.ContainsRune(" .,:/-", r) {
					b.WriteRune(r)
				} else {
					b.WriteString(`\` + string(r)) // any other character as itself
				}
			}
			continue
		}
		// Excel reads m and mm as minutes after hours or before seconds, else as months.
		minutes := strings.HasPrefix(prev, "h") || strings.HasPrefix(prev, "H") || strings.HasPrefix(next(parts, i), "s")
		switch p.Letters {
		case "mm", "m":
			if !minutes {
				return fail("Excel shows minutes only after hours or before seconds")
			}
		case "MM", "M":
			if minutes {
				return fail("Excel shows a month after hours or before seconds as minutes")
			}
		case "SSS":
			if prev != "ss" || i == 0 || parts[i-1].Text != "." {
				return fail("Excel shows milliseconds only as ss.SSS")
			}
		}
		b.WriteString(dateLetters[p.Letters])
		prev = p.Letters
	}
	return b.String(), nil
}

// next returns the letters of the next part with letters after i.
func next(parts []expr.DatePart, i int) string {
	for _, p := range parts[i+1:] {
		if p.Letters != "" {
			return p.Letters
		}
	}
	return ""
}
