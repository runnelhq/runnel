// Package sqldb opens the databases of connections (ADR 0022) and runs statements with
// parameters: values only as @name, never as text in the statement. Databases are kept
// open in a pool per connection settings for the life of the process.
package sqldb

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"  // the driver "pgx" (pure Go, MIT)
	_ "github.com/microsoft/go-mssqldb" // the driver "sqlserver" (pure Go, BSD-3; Windows authentication on Windows)
	_ "modernc.org/sqlite"              // the driver "sqlite" (pure Go, BSD-3)

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
var Types = []string{connections.TypeSQLite, connections.TypeSQLServer, connections.TypePostgres}

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
	switch c.Type {
	case connections.TypeSQLite:
		return sqliteSource(ctx, reg, c)
	case connections.TypeSQLServer:
		return sqlServerSource(reg, c)
	case connections.TypePostgres:
		return postgresSource(reg, c)
	}
	return "", "", fmt.Errorf("connection %s: type %s is not a database", c.Name, c.Type)
}

func sqliteSource(ctx context.Context, reg *connection.Registry, c connection.Resolved) (string, string, error) {
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

func sqlServerSource(reg *connection.Registry, c connection.Resolved) (string, string, error) {
	var cfg connections.SQLServer
	if err := reg.Decode(c, &cfg); err != nil {
		return "", "", err
	}
	timeout, err := seconds(cfg.Timeout)
	if err != nil {
		return "", "", fmt.Errorf("connection %s: timeout: %w", c.Name, err)
	}
	u := url.URL{Scheme: "sqlserver", Host: cfg.Host}
	switch {
	case cfg.Port != "" && cfg.Instance != "":
		return "", "", fmt.Errorf("connection %s: give either port or instance, not both", c.Name)
	case cfg.Port != "":
		if err := checkPort(cfg.Port); err != nil {
			return "", "", fmt.Errorf("connection %s: %w", c.Name, err)
		}
		u.Host = net.JoinHostPort(cfg.Host, cfg.Port)
	case cfg.Instance != "":
		u.Path = cfg.Instance
	}
	switch {
	case cfg.User != "":
		u.User = url.UserPassword(cfg.User, cfg.Password)
	case runtime.GOOS != "windows":
		return "", "", fmt.Errorf("connection %s: Windows authentication works only on Windows; set user and password", c.Name)
	}
	q := url.Values{}
	q.Set("database", cfg.Database)
	q.Set("encrypt", cmp.Or(cfg.Encrypt, "true"))
	q.Set("TrustServerCertificate", strconv.FormatBool(cfg.TrustServerCertificate))
	q.Set("connection timeout", timeout)
	q.Set("app name", "Runnel")
	u.RawQuery = q.Encode()
	return "sqlserver", u.String(), nil
}

func postgresSource(reg *connection.Registry, c connection.Resolved) (string, string, error) {
	var cfg connections.Postgres
	if err := reg.Decode(c, &cfg); err != nil {
		return "", "", err
	}
	timeout, err := seconds(cfg.Timeout)
	if err != nil {
		return "", "", fmt.Errorf("connection %s: timeout: %w", c.Name, err)
	}
	port := cmp.Or(cfg.Port, "5432")
	if err := checkPort(port); err != nil {
		return "", "", fmt.Errorf("connection %s: %w", c.Name, err)
	}
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(cfg.Host, port), Path: "/" + cfg.Database,
		User: url.UserPassword(cfg.User, cfg.Password)}
	q := url.Values{}
	q.Set("sslmode", cmp.Or(cfg.SSLMode, "verify-full"))
	q.Set("connect_timeout", timeout)
	q.Set("application_name", "Runnel")
	u.RawQuery = q.Encode()
	return "pgx", u.String(), nil
}

// seconds returns a connect timeout (default 30s) in whole seconds as text.
func seconds(s string) (string, error) {
	if s == "" {
		return "30", nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < time.Second {
		return "", fmt.Errorf("invalid duration %q (at least 1s, e.g. 30s)", s)
	}
	return strconv.Itoa(int(d / time.Second)), nil
}

func checkPort(port string) error {
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("invalid port %q (expected a number from 1 to 65535)", port)
	}
	return nil
}

// Statement returns the statement and its arguments for the driver of a connection:
// parameters @name as named arguments, for PostgreSQL rewritten to $1, $2, … (pgx has
// no named arguments).
func Statement(c connection.Resolved, stmt string, params map[string]string) (string, []any, error) {
	args, err := Args(stmt, params)
	if err != nil || c.Type != connections.TypePostgres {
		return stmt, args, err
	}
	var b strings.Builder
	var values []any
	index := map[string]int{}
	last := 0
	for _, p := range placeholders(stmt) {
		n, ok := index[p.name]
		if !ok {
			values = append(values, params[p.name])
			n = len(values)
			index[p.name] = n
		}
		b.WriteString(stmt[last:p.start])
		b.WriteString("$" + strconv.Itoa(n))
		last = p.end
	}
	b.WriteString(stmt[last:])
	return b.String(), values, nil
}

// Timeout reads the timeout of a statement; empty is DefaultTimeout.
func Timeout(s string) (time.Duration, error) {
	if s == "" {
		return DefaultTimeout, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid timeout %q (examples: 30s, 5m)", s)
	}
	return d, nil
}
