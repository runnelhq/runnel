package format

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseSize reads a size like 500KB, 10MB, 2GB or 1024 (bytes) for a field such as
// maxSize or maxOutput (ADR 0025: sizes are text); empty gives fallback.
func ParseSize(field, text string, fallback int64) (int64, error) {
	s := strings.ToUpper(strings.TrimSpace(text))
	if s == "" {
		return fallback, nil
	}
	unit := int64(1)
	for _, u := range []struct {
		suffix string
		factor int64
	}{{"KB", 1 << 10}, {"MB", 1 << 20}, {"GB", 1 << 30}, {"B", 1}} {
		if before, ok := strings.CutSuffix(s, u.suffix); ok {
			s, unit = strings.TrimSpace(before), u.factor
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 || n > (1<<50)/unit {
		return 0, fmt.Errorf("%s: %q is not a size like 500KB, 10MB or 2GB", field, text)
	}
	return n * unit, nil
}
