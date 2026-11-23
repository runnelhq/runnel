package db

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/sqldb"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

// testDB creates an SQLite database with the statements and returns its file.
func testDB(t testing.TB, stmts ...string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "test.db")
	db, err := sqldb.Open(context.Background(), connections.Builtin(), resolved(file))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	return file
}

func resolved(file string) connection.Resolved {
	return connection.Resolved{Name: "test", Type: connections.TypeSQLite, With: map[string]any{"path": file}}
}

// read runs db.table-input on the database file and returns its records.
func read(ctx context.Context, t testing.TB, file string, with map[string]any) ([]record.Record, error) {
	t.Helper()
	return readResolved(ctx, resolved(file), with)
}

// readResolved runs db.table-input on a resolved connection.
func readResolved(ctx context.Context, conn connection.Resolved, with map[string]any) ([]record.Record, error) {
	with["connection"] = "test"
	tr, err := newTableInput(with)
	if err != nil {
		return nil, err
	}
	var out []record.Record
	o := transform.NewOutput("in", func(r record.Record) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		out = append(out, r)
		return nil
	}, func(slog.Level, string) {}).WithConnections(func(context.Context, string) (connection.Resolved, error) {
		return conn, nil
	})
	in := make(chan transform.Batch)
	close(in)
	return out, tr.Process(ctx, in, o)
}

const orders = `CREATE TABLE orders (id INTEGER PRIMARY KEY, customer TEXT, amount DECIMAL(10,2), day DATE,
	at DATETIME, paid BOOLEAN, data BLOB, note TEXT)`

func TestTableInput(t *testing.T) {
	file := testDB(t, orders,
		`INSERT INTO orders VALUES (1, 'Ada', 12.50, '2026-11-19', '2026-11-19 08:30:00', 1, x'0102', NULL)`,
		`INSERT INTO orders VALUES (2, 'Grace', '1234.10', '2026-11-20', '2026-11-20T09:15:00Z', 0, NULL, 'x')`,
	)
	recs, err := read(context.Background(), t, file, map[string]any{"sql": "SELECT * FROM orders ORDER BY id"})
	if err != nil || len(recs) != 2 {
		t.Fatalf("%v, %v", recs, err)
	}
	r := recs[0]
	if got := strings.Join(r.Names(), ","); got != "id,customer,amount,day,at,paid,data,note" {
		t.Errorf("fields %s", got)
	}
	want := map[string]any{
		"id": int64(1), "customer": "Ada", "amount": decimal.RequireFromString("12.5"),
		"day": record.DateOf(time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)), "paid": true, "note": nil,
	}
	for name, w := range want {
		v, _ := r.Get(name)
		if d, ok := w.(decimal.Decimal); ok {
			if got, ok := v.(decimal.Decimal); !ok || !got.Equal(d) {
				t.Errorf("%s = %#v, want %s", name, v, d)
			}
			continue
		}
		if v != w {
			t.Errorf("%s = %#v (%T), want %#v", name, v, v, w)
		}
	}
	if at, _ := r.Get("at"); record.TypeOf(at) != record.DateTime {
		t.Errorf("at = %#v", at)
	}
	if data, _ := r.Get("data"); string(data.([]byte)) != "\x01\x02" {
		t.Errorf("data = %#v", data)
	}
	if amount, _ := recs[1].Get("amount"); amount.(decimal.Decimal).String() != "1234.1" {
		t.Errorf("amount from text = %v", amount)
	}

	// Values as parameters; a value with SQL in it is only a value.
	for value, n := range map[string]int{"Ada": 1, "x' OR '1'='1": 0} {
		recs, err := read(context.Background(), t, file, map[string]any{"sql": "SELECT id FROM orders WHERE customer = @name", "params": map[string]any{"name": value}})
		if err != nil || len(recs) != n {
			t.Errorf("%q: %d records, %v", value, len(recs), err)
		}
	}
}

func TestTableInputErrors(t *testing.T) {
	file := testDB(t, orders)
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"sql": "SELECT id, id FROM orders"}, `the query returns the column "id" twice`},
		{map[string]any{"sql": "SELECT * FROM missing"}, "connection test: SQL logic error: no such table: missing"},
		{map[string]any{"sql": "SELECT * FROM orders WHERE id = @id"}, "parameter @id has no value under params"},
		{map[string]any{"sql": "SELECT 1", "timeout": "-1s"}, `invalid timeout "-1s"`},
		{map[string]any{
			"sql":     "WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c) SELECT count(*) FROM c",
			"timeout": "200ms",
		}, "the query took longer than 200ms"},
	} {
		if _, err := read(context.Background(), t, file, tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
}

// Stopping the pipeline stops reading.
func TestTableInputCancel(t *testing.T) {
	file := testDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	start := time.Now()
	_, err := read(ctx, t, file, map[string]any{"sql": "WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c) SELECT x FROM c"})
	if err != context.Canceled || time.Since(start) > 5*time.Second { //nolint:errorlint // the error of the context itself
		t.Errorf("%v after %s", err, time.Since(start))
	}
	// Without connections (no project) it says so.
	tr := transformtest.New(t, tableInputDefinition, map[string]any{"connection": "x", "sql": "SELECT 1"})
	if _, _, err := transformtest.Run(context.Background(), tr); err != connection.ErrNotAvailable { //nolint:errorlint // the sentinel itself
		t.Errorf("no connections: %v", err)
	}
}

// BenchmarkTableInput reads 100,000 rows with four columns (per-record cost of the input).
func BenchmarkTableInput(b *testing.B) {
	file := testDB(b, "CREATE TABLE t (id INTEGER, name TEXT, amount DECIMAL(10,2), day DATE)",
		`INSERT INTO t WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c LIMIT 100000)
		 SELECT x, 'name ' || x, x * 1.25, '2026-11-23' FROM c`)
	b.ReportAllocs()
	for b.Loop() {
		recs, err := read(context.Background(), b, file, map[string]any{"sql": "SELECT * FROM t"})
		if err != nil || len(recs) != 100000 {
			b.Fatal(len(recs), err)
		}
	}
}
