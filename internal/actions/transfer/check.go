package transfer

import (
	"context"
	"fmt"

	"github.com/runnelhq/runnel/internal/connection"
)

// Check signs in to the server of an sftp, ftps or ftp connection and lists the folder
// after the sign-in, without writing anything (Test connection in the editor, ADR 0022
// section 8). An unknown host key or certificate fails with the fingerprint to check.
func Check(ctx context.Context, c connection.Resolved) (string, error) {
	s, err := connect(ctx, c)
	if err != nil {
		return "", err
	}
	defer s.close()
	dir, err := s.fs.Getwd()
	if err != nil {
		dir = "."
	}
	entries, err := s.fs.ReadDir(".")
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("connection %s: signed in, but cannot read the folder %s: %w", c.Name, dir, plain(err))
	}
	word := "entries"
	if len(entries) == 1 {
		word = "entry"
	}
	return fmt.Sprintf("signed in to %s as %s; folder %s has %d %s", s.addr, s.user, dir, len(entries), word), nil
}
