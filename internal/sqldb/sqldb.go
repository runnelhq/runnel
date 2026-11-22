// Package sqldb opens the databases of connections (ADR 0022) and runs statements with
// parameters: values only as @name, never as text in the statement. Databases are kept
// open in a pool per connection settings for the life of the process.
package sqldb

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite" // the driver "sqlite" (pure Go, BSD-3)

	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/paths"
)

// MaxOpen is the most connections a pool keeps open to one database (ADR 0022).
const MaxOpen = 4

// DefaultTimeout is the time a statement may take unless the block sets another.
const DefaultTimeout = 5 * time.Minute

var (
	poolMu sync.Mutex
	pools  = map[string]*sql.DB{}
)

// Types are the connection types this package can open.
var Types = []string{connections.TypeSQLite}

// Open returns the pooled database of a resolved connection; relative paths refer to the
// base folder of the run (ADR 0020).
func Open(ctx context.Context, reg *connection.Registry, c connection.Resolved) (*sql.DB, error) {
	driver, dsn, err := source(ctx, reg, c)
	if err != nil {
		return nil, err
	}
	key := driver + "\x00" + dsn
	poolMu.Lock()
	defer poolMu.Unlock()
	if db := pools[key]; db != nil {
		return db, nil
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("connection %s: %w", c.Name, err)
	}
	db.SetMaxOpenConns(MaxOpen)
	db.SetConnMaxIdleTime(5 * time.Minute)
	if driver == "sqlite" {
		// No idle connections: the file is free again after each statement, so that it
		// can be moved or deleted while the server runs (Windows locks open files).
		db.SetMaxIdleConns(0)
	}
	pools[key] = db
	return db, nil
}

// source returns the driver and data source name of a connection.
func source(ctx context.Context, reg *connection.Registry, c connection.Resolved) (driver, dsn string, err error) {
	if c.Type != connections.TypeSQLite { // SQL Server and PostgreSQL follow (S20c)
		return "", "", fmt.Errorf("connection %s: type %s is not a database", c.Name, c.Type)
	}
	var cfg connections.SQLite
	if err := reg.Decode(c, &cfg); err != nil {
		return "", "", err
	}
	path, err := filepath.Abs(paths.Resolve(ctx, cfg.Path))
	if err != nil {
		return "", "", err
	}
	if !cfg.ReadOnly {
		// A new database file needs its folder, e.g. out/ of a fresh project.
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", "", fmt.Errorf("connection %s: %w", c.Name, err)
		}
	}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	if cfg.ReadOnly {
		q.Set("mode", "ro")
	}
	return "sqlite", "file:" + filepath.ToSlash(path) + "?" + q.Encode(), nil
}
