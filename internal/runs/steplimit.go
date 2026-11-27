package runs

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/runnelhq/runnel/internal/vars"
)

// Limits of the data kept per step for the editor (E23a).
const (
	// MaxStepBytes limits the input and, separately, the output of a step: of one
	// execution, or of one entry of forEach (estimated size as JSON).
	MaxStepBytes = 1 << 20
	// MaxStepDataBytes limits all data kept for a run; further steps keep their status only.
	MaxStepDataBytes = 20 << 20
	// maxErrorBytes limits an error message kept for a step.
	maxErrorBytes = 16 << 10
)

// Truncation tells where the data of a step was cut to stay within MaxStepBytes (E23a).
type Truncation struct {
	// Part is input, output or row.
	Part string `json:"part"`
	// Path is the place in the part, written like in expressions, e.g. items[2].body;
	// empty: the part itself.
	Path string `json:"path,omitempty"`
	// Kind is list, object or text.
	Kind string `json:"kind"`
	// Length is the real number of entries, keys or bytes; Kept how many were kept.
	Length int `json:"length"`
	Kept   int `json:"kept"`
}

// limiter copies a value for the step data: secrets masked, cut to its budget. It walks
// the value in a fixed order (keys sorted), so the same value is always cut alike:
// lists and objects keep their first entries, texts their beginning.
type limiter struct {
	mask *vars.Mask
	part string
	left int // bytes left (estimated size as JSON)
	used int
	cuts []Truncation
}

// limitValue returns a masked copy of v within budget bytes, the bytes it takes and the
// places where it was cut.
func limitValue(v any, part string, budget int, mask *vars.Mask) (any, int, []Truncation) {
	l := &limiter{mask: mask, part: part, left: budget}
	out := l.value(v, "")
	return out, l.used, l.cuts
}

func (l *limiter) take(n int) {
	l.left -= n
	l.used += n
}

func (l *limiter) cut(path, kind string, length, kept int) {
	l.cuts = append(l.cuts, Truncation{Part: l.part, Path: path, Kind: kind, Length: length, Kept: kept})
}

func (l *limiter) value(v any, path string) any {
	switch v := v.(type) {
	case nil:
		l.take(4)
		return nil
	case bool:
		l.take(5)
		return v
	case string:
		return l.text(v, path)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		l.take(8)
		return v
	case time.Time:
		l.take(32)
		return v
	case map[string]any:
		return l.object(path, slices.Sorted(maps.Keys(v)), func(k string) any { return v[k] })
	case map[string]string:
		return l.object(path, slices.Sorted(maps.Keys(v)), func(k string) any { return v[k] })
	case []any:
		return l.list(path, len(v), func(i int) any { return v[i] })
	case []map[string]any:
		return l.list(path, len(v), func(i int) any { return v[i] })
	case []string:
		return l.list(path, len(v), func(i int) any { return v[i] })
	}
	// Other types (rare: actions return texts, numbers, lists and maps) as the API
	// would write them.
	data, err := json.Marshal(v)
	if err != nil {
		return l.text(fmt.Sprint(v), path)
	}
	var x any
	if err := json.Unmarshal(data, &x); err != nil {
		return l.text(string(data), path)
	}
	return l.value(x, path)
}

func (l *limiter) text(s, path string) string {
	s = l.mask.String(s)
	if len(s)+2 <= l.left {
		l.take(len(s) + 2)
		return s
	}
	keep := max(l.left-2, 0)
	for keep > 0 && !utf8.RuneStart(s[keep]) {
		keep--
	}
	l.cut(path, "text", len(s), keep)
	l.take(keep + 2)
	l.left = min(l.left, 0) // nothing more fits
	return s[:keep]
}

func (l *limiter) object(path string, keys []string, get func(string) any) map[string]any {
	out := make(map[string]any, len(keys))
	l.take(2)
	for i, k := range keys {
		if l.left <= 0 {
			l.cut(path, "object", len(keys), i)
			break
		}
		mk := l.mask.String(k)
		l.take(len(mk) + 4)
		out[mk] = l.value(get(k), keyPath(path, mk))
	}
	return out
}

func (l *limiter) list(path string, n int, get func(int) any) []any {
	out := make([]any, 0, min(n, 1024))
	l.take(2)
	for i := range n {
		if l.left <= 0 {
			l.cut(path, "list", n, i)
			break
		}
		l.take(1)
		out = append(out, l.value(get(i), path+"["+strconv.Itoa(i)+"]"))
	}
	return out
}

// keyPath appends a key to a path like expressions write it: items.name, or
// items["first name"] for a key that is no name.
func keyPath(path, key string) string {
	if !isName(key) {
		return path + "[" + strconv.Quote(key) + "]"
	}
	if path == "" {
		return key
	}
	return path + "." + key
}

func isName(s string) bool {
	for i, c := range s {
		if c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return s != ""
}

// limitError returns the masked text of an error, cut to maxErrorBytes; empty if nil.
func limitError(err error, mask *vars.Mask) string {
	if err == nil {
		return ""
	}
	return limitText(mask.String(err.Error()))
}

func limitText(s string) string {
	if len(s) <= maxErrorBytes {
		return s
	}
	keep := maxErrorBytes
	for keep > 0 && !utf8.RuneStart(s[keep]) {
		keep--
	}
	return s[:keep] + "…"
}
