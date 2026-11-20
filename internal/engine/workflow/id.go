package workflow

import (
	"time"

	"github.com/runnelhq/runnel/internal/engine/runid"
)

// NewRunID returns a new run id (a ULID, see package runid).
func NewRunID(now time.Time) string {
	return runid.New(now)
}
