package model

import (
	"fmt"
	"time"
)

// Duration is a time span as written in a file, e.g. "30s", "5m", "2h" or "1h30m".
// It keeps the original text; Value parses it.
type Duration string

// Value returns the parsed duration. An empty Duration is 0.
func (d Duration) Value() (time.Duration, error) {
	if d == "" {
		return 0, nil
	}
	v, err := time.ParseDuration(string(d))
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q (examples: 30s, 5m, 2h, 1h30m)", string(d))
	}
	if v < 0 {
		return 0, fmt.Errorf("duration %q must not be negative", string(d))
	}
	return v, nil
}
