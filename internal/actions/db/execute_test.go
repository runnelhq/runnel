package db

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/sqldb"
)

// execute runs db.execute on the SQLite database file of the connection "test".
func execute(t *testing.T, file string, with map[string]any) (action.Result, []string, error) {
	t.Helper()
	with["connection"] = "test"
	a, err := newExecute(with)
	if err != nil {
		return action.Result{}, nil, err
	}
	var logs []string
	ec := &action.ExecContext{
		Log: func(_ slog.Level, msg string) { logs = append(logs, msg) },
		Connection: func(_ context.Context, name string) (connection.Resolved, error) {
			if name != "test" {
				return connection.Resolved{}, errors.New("unknown")
			}
			return connection.Resolved{Name: name, Type: connections.TypeSQLite, With: map[string]any{"path": file}}, nil
		},
	}
	res, err := a.Execute(context.Background(), ec)
	return res, logs, err
}

func TestExecute(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sub", "test.db") // the folder is created
	if _, _, err := execute(t, file, map[string]any{"sql": "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)"}); err != nil {
		t.Fatal(err)
	}
	// A value with SQL in it stays a value: parameters cannot change the statement.
	for _, name := range []string{"Ada", "x'); DROP TABLE t; --"} {
		res, logs, err := execute(t, file, map[string]any{"sql": "INSERT INTO t (name) VALUES (@name)", "params": map[string]any{"name": name}})
		if err != nil || res.Output.(map[string]any)["rowsAffected"] != int64(1) || logs[0] != "1 row affected" {
			t.Fatalf("insert %q: %v %v %v", name, res, logs, err)
		}
	}
	res, logs, err := execute(t, file, map[string]any{"sql": "UPDATE t SET name = upper(name) WHERE name <> @keep", "params": map[string]any{"keep": "none"}})
	if err != nil || res.Output.(map[string]any)["rowsAffected"] != int64(2) || logs[0] != "2 rows affected" {
		t.Errorf("update: %v %v %v", res, logs, err)
	}

	db, err := sqldb.Open(context.Background(), connections.Builtin(), connection.Resolved{Name: "test", Type: connections.TypeSQLite, With: map[string]any{"path": file}})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow("SELECT count(*) FROM t WHERE name = 'X''); DROP TABLE T; --'").Scan(&n); err != nil || n != 1 {
		t.Errorf("the value with SQL in it was stored as text: n=%d, %v", n, err)
	}
}

func TestExecuteErrors(t *testing.T) {
	file := filepath.Join(t.TempDir(), "test.db")
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"sql": "DELETE FROM t WHERE id = @id"}, "parameter @id has no value under params"},
		{map[string]any{"sql": "DELETE FROM t", "params": map[string]any{"id": "1"}}, "params: id is not used in the statement"},
		{map[string]any{"sql": "SELECT 1", "timeout": "soon"}, `invalid timeout "soon"`},
		{map[string]any{"sql": "INSERT INTO missing VALUES (1)"}, "connection test: SQL logic error: no such table: missing"},
	} {
		if _, _, err := execute(t, file, tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
	// Without a project there are no connections.
	a, _ := newExecute(map[string]any{"connection": "x", "sql": "SELECT 1"})
	if _, err := a.Execute(context.Background(), &action.ExecContext{}); !errors.Is(err, connection.ErrNotAvailable) {
		t.Errorf("no connections: %v", err)
	}
}

// A statement that runs too long is stopped after the timeout (ADR 0022).
func TestExecuteTimeout(t *testing.T) {
	file := filepath.Join(t.TempDir(), "test.db")
	start := time.Now()
	_, _, err := execute(t, file, map[string]any{
		"sql":     "WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c) SELECT count(*) FROM c",
		"timeout": "200ms",
	})
	if err == nil || err.Error() != "the statement took longer than 200ms" || time.Since(start) > 5*time.Second {
		t.Errorf("%v after %s", err, time.Since(start))
	}
}
