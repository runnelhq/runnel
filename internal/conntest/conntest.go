// Package conntest tests a connection for the editor (ADR 0022, section 8; S39c1): it
// opens the connection like a run would and does the least that shows it works –
// databases answer a ping and their version, an http API answers the base address with
// the sign-in, a mail server accepts the sign-in. Nothing is written or sent.
package conntest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/runnelhq/runnel/internal/actions/http"
	"github.com/runnelhq/runnel/internal/actions/mail"
	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/sqldb"
)

// Run tests a resolved connection and returns what it found, e.g. the version of the
// database. Relative paths (SQLite) refer to the base folder in ctx (paths.WithBase).
func Run(ctx context.Context, c connection.Resolved) (string, error) {
	switch c.Type {
	case connections.TypeSQLite, connections.TypeSQLServer, connections.TypePostgres:
		return database(ctx, c)
	case connections.TypeHTTP:
		return http.Check(ctx, c)
	case connections.TypeSMTP:
		return mail.Check(ctx, c)
	}
	return "", fmt.Errorf("connections of type %s cannot be tested yet", c.Type)
}

// versionQuery reads the version of the database server.
var versionQuery = map[string]string{
	connections.TypeSQLite:    "SELECT 'SQLite ' || sqlite_version()",
	connections.TypeSQLServer: "SELECT 'SQL Server ' + CAST(SERVERPROPERTY('ProductVersion') AS nvarchar(128))",
	connections.TypePostgres:  "SELECT 'PostgreSQL ' || current_setting('server_version')",
}

func database(ctx context.Context, c connection.Resolved) (string, error) {
	if c.Type == connections.TypeSQLite {
		// A test does not create the file: a run does that on first use.
		var cfg connections.SQLite
		if err := connections.Builtin().Decode(c, &cfg); err != nil {
			return "", err
		}
		file := paths.Resolve(ctx, cfg.Path)
		if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
			return fmt.Sprintf("the database file %s does not exist yet; a run creates it", filepath.ToSlash(cfg.Path)), nil
		}
	}
	db, err := sqldb.Open(ctx, connections.Builtin(), c)
	if err != nil {
		return "", err
	}
	if err := db.PingContext(ctx); err != nil {
		return "", fmt.Errorf("connection %s: %w", c.Name, err)
	}
	var version sql.NullString
	if err := db.QueryRowContext(ctx, versionQuery[c.Type]).Scan(&version); err != nil {
		return "", fmt.Errorf("connection %s: %w", c.Name, err)
	}
	return version.String, nil
}
