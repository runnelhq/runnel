package http

import (
	"context"
	"fmt"
	"net/http"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
)

// Check sends GET to the base address of an http connection with its headers and
// sign-in (test of a connection in the editor, S39c1). Any answer means the API was
// reached; 401 and 403 mean the sign-in was refused.
func Check(ctx context.Context, c connection.Resolved) (string, error) {
	p := &api{name: c.Name}
	if err := connections.Builtin().Decode(c, &p.cfg); err != nil {
		return "", err
	}
	if err := p.check(); err != nil {
		return "", fmt.Errorf("connection %s: %w", c.Name, err)
	}
	a := &requestAction{Method: http.MethodGet}
	retries := 0
	a.RetryAfter = &retryAfterConfig{MaxRetries: &retries}
	cl := &call{a: a, ec: &action.ExecContext{}, api: p, method: http.MethodGet, timeout: defaultTimeout, maxSize: defaultMaxSize}
	ans, err := cl.fetch(ctx, p.base)
	if err != nil {
		return "", err
	}
	where := cl.where(p.base)
	switch ans.status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", fmt.Errorf("%s: the API refused the sign-in (%s)", where, ans.text)
	}
	return fmt.Sprintf("%s answered %s", where, ans.text), nil
}
