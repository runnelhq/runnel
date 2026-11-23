package mail

import (
	"context"
	"fmt"

	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
)

// Check connects to the mail server of an smtp connection and signs in, as mail.send
// does, but sends no mail (test of a connection in the editor, S39c1). It returns what
// was checked.
func Check(ctx context.Context, c connection.Resolved) (string, error) {
	var cfg connections.SMTP
	if err := connections.Builtin().Decode(c, &cfg); err != nil {
		return "", err
	}
	srv, err := newServer(c.Name, cfg)
	if err != nil {
		return "", fmt.Errorf("connection %s: %w", c.Name, err)
	}
	if err := srv.send(ctx, "", nil, nil); err != nil {
		return "", err
	}
	if srv.user == "" {
		return fmt.Sprintf("connected to %s (%s, no sign-in)", srv.addr, srv.tls), nil
	}
	return fmt.Sprintf("signed in to %s as %s (%s)", srv.addr, srv.user, srv.tls), nil
}
