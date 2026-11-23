package db

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/record"
)

// query runs db.query on the SQLite database file of the connection "test".
func query(t *testing.T, file string, with map[string]any) (map[string]any, []string, error) {
	t.Helper()
	with["connection"] = "test"
	a, err := newQuery(with)
	if err != nil {
		return nil, nil, err
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
	out, _ := res.Output.(map[string]any)
	return out, logs, err
}

func ordersDB(t *testing.T) string {
	file := filepath.Join(t.TempDir(), "test.db")
	for _, stmt := range []string{
		"CREATE TABLE orders (id INTEGER, status TEXT, amount DECIMAL(10,2), day DATE)",
		"INSERT INTO orders VALUES (1, 'open', 10.5, '2026-11-23'), (2, 'open', 20, '2026-11-24'), (3, 'done', 5, NULL)",
	} {
		if _, _, err := execute(t, file, map[string]any{"sql": stmt}); err != nil {
			t.Fatal(err)
		}
	}
	return file
}

func TestQuery(t *testing.T) {
	file := ordersDB(t)
	out, logs, err := query(t, file, map[string]any{"sql": "SELECT id, amount, day FROM orders WHERE status = @status ORDER BY id", "params": map[string]any{"status": "open"}})
	if err != nil || logs[0] != "read 2 rows" {
		t.Fatalf("%v %v", logs, err)
	}
	rows := out["rows"].([]any)
	if out["count"] != int64(2) || len(rows) != 2 || out["value"] != int64(1) {
		t.Fatalf("%#v", out)
	}
	first := out["first"].(map[string]any)
	if !first["amount"].(decimal.Decimal).Equal(decimal.RequireFromString("10.5")) || record.TypeOf(first["day"]) != record.DateType {
		t.Errorf("types: %#v", first)
	}
	// A count as value; no rows: count 0, first and value empty.
	if out, _, _ := query(t, file, map[string]any{"sql": "SELECT COUNT(*) FROM orders"}); out["value"] != int64(3) {
		t.Errorf("count: %#v", out)
	}
	out, logs, err = query(t, file, map[string]any{"sql": "SELECT * FROM orders WHERE id > 99"})
	if err != nil || out["count"] != int64(0) || out["first"] != nil || out["value"] != nil || len(out["rows"].([]any)) != 0 || logs[0] != "read 0 rows" {
		t.Errorf("empty: %#v %v %v", out, logs, err)
	}
}

func TestQueryErrors(t *testing.T) {
	file := ordersDB(t)
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"sql": "SELECT * FROM orders", "maxRows": "2"}, "the query returns more than 2 rows (maxRows); read larger results in a pipeline with db.table-input"},
		{map[string]any{"sql": "SELECT * FROM orders", "maxRows": "none"}, `maxRows "none" is not a whole number of at least 1`},
		{map[string]any{"sql": "SELECT id, id FROM orders"}, `the query returns the column "id" twice`},
		{map[string]any{"sql": "SELECT * FROM missing"}, "connection test: SQL logic error: no such table: missing"},
		{map[string]any{"sql": "SELECT * FROM orders WHERE id = @id"}, "parameter @id has no value under params"},
		{map[string]any{"sql": "SELECT 1", "timeout": "soon"}, `invalid timeout "soon"`},
	} {
		if _, _, err := query(t, file, tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
	// Outside a project there are no connections.
	a, _ := newQuery(map[string]any{"connection": "x", "sql": "SELECT 1"})
	if _, err := a.Execute(context.Background(), &action.ExecContext{Log: func(slog.Level, string) {}}); !errors.Is(err, connection.ErrNotAvailable) {
		t.Errorf("%v", err)
	}
}
