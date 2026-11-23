package db

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/sqldb"
)

// TestPostgres runs against a real PostgreSQL server when RUNNEL_TEST_POSTGRES holds
// host:port:database:user:password (see docs/TESTPLAN.md, S20c); sslMode is disable.
func TestPostgres(t *testing.T) {
	spec := os.Getenv("RUNNEL_TEST_POSTGRES")
	if spec == "" {
		t.Skip("RUNNEL_TEST_POSTGRES is not set (host:port:database:user:password)")
	}
	f := strings.SplitN(spec, ":", 5)
	if len(f) != 5 {
		t.Fatal("RUNNEL_TEST_POSTGRES: expected host:port:database:user:password")
	}
	conn := connection.Resolved{Name: "pg", Type: connections.TypePostgres, With: map[string]any{
		"host": f[0], "port": f[1], "database": f[2], "user": f[3], "password": f[4], "sslMode": "disable",
	}}
	ctx := context.Background()
	db, err := sqldb.Open(ctx, connections.Builtin(), conn)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(stmt string, params map[string]string) {
		t.Helper()
		q, args, err := sqldb.Statement(conn, stmt, params)
		if err == nil {
			_, err = db.ExecContext(ctx, q, args...)
		}
		if err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	exec("DROP TABLE IF EXISTS runnel_test", nil)
	exec(`CREATE TABLE runnel_test (id SERIAL PRIMARY KEY, name TEXT, amount NUMERIC(12,2), day DATE,
		at TIMESTAMPTZ, paid BOOLEAN, ref UUID, data JSONB)`, nil)
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS runnel_test") })
	// One parameter used twice: both places get $1.
	exec(`INSERT INTO runnel_test (name, amount, day, at, paid, ref, data)
		VALUES (@name, @amount, @day, @at, @paid, '6f1c1d3e-6b0e-4d5e-9a3c-2b1f0e9d8c7b', jsonb_build_object('who', @name::text))`,
		map[string]string{"name": "Ada", "amount": "1234.50", "day": "2026-11-23", "at": "2026-11-23T08:30:00Z", "paid": "true"})
	exec("INSERT INTO runnel_test (name) VALUES (@name)", map[string]string{"name": "x' OR '1'='1"})

	recs, err := readResolved(ctx, conn, map[string]any{"sql": "SELECT name, amount, day, at, paid, ref, data FROM runnel_test WHERE name = @name OR name = @other ORDER BY id",
		"params": map[string]any{"name": "Ada", "other": "nobody"}})
	if err != nil || len(recs) != 1 {
		t.Fatalf("%v, %v", recs, err)
	}
	r := recs[0]
	if v, _ := r.Get("amount"); !v.(decimal.Decimal).Equal(decimal.RequireFromString("1234.5")) {
		t.Errorf("amount = %#v", v)
	}
	for name, want := range map[string]record.Type{"name": record.String, "day": record.DateType, "at": record.DateTime, "paid": record.Bool, "ref": record.String} {
		if v, _ := r.Get(name); record.TypeOf(v) != want {
			t.Errorf("%s = %#v (%s), want %s", name, v, record.TypeOf(v), want)
		}
	}
	if v, _ := r.Get("ref"); v != "6f1c1d3e-6b0e-4d5e-9a3c-2b1f0e9d8c7b" {
		t.Errorf("ref = %#v", v)
	}
}

// TestPostgresUpsert writes with db.upsert (S21) on PostgreSQL: $n placeholders, ON
// CONFLICT, dates and JSON; like TestPostgres only with RUNNEL_TEST_POSTGRES.
func TestPostgresUpsert(t *testing.T) {
	spec := os.Getenv("RUNNEL_TEST_POSTGRES")
	if spec == "" {
		t.Skip("RUNNEL_TEST_POSTGRES is not set (host:port:database:user:password)")
	}
	f := strings.SplitN(spec, ":", 5)
	conn := connection.Resolved{Name: "pg", Type: connections.TypePostgres, With: map[string]any{
		"host": f[0], "port": f[1], "database": f[2], "user": f[3], "password": f[4], "sslMode": "disable",
	}}
	ctx := context.Background()
	db, err := sqldb.Open(ctx, connections.Builtin(), conn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS runnel_upsert; CREATE TABLE runnel_upsert (id INT PRIMARY KEY, customer TEXT, amount NUMERIC(10,2), day DATE, tags JSONB)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS runnel_upsert") })
	rec := func(customer, amount string) record.Record {
		r := order(1, customer, amount)
		r.Set("day", record.DateOf(time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)))
		r.Set("tags", []any{"a"})
		return r
	}
	for _, r := range []record.Record{rec("Ada", "1.50"), rec("Grace", "2.50")} {
		if _, _, _, err := write(t, upsertDefinition, conn, map[string]any{"table": "runnel_upsert", "keys": []any{"id"}}, false, r); err != nil {
			t.Fatal(err)
		}
	}
	recs, err := readResolved(ctx, conn, map[string]any{"sql": "SELECT customer, amount, day, tags FROM runnel_upsert"})
	if err != nil || len(recs) != 1 {
		t.Fatalf("%v, %v", recs, err)
	}
	if v, _ := recs[0].Get("customer"); v != "Grace" {
		t.Errorf("customer = %#v", v)
	}
	if v, _ := recs[0].Get("amount"); !v.(decimal.Decimal).Equal(decimal.RequireFromString("2.5")) {
		t.Errorf("amount = %#v", v)
	}
	// db.lookup (S23a) with $1 on the same table.
	looked, _, _, err := write(t, lookupDefinition, conn, map[string]any{"table": "runnel_upsert",
		"keys": []any{map[string]any{"field": "id"}}, "return": []any{map[string]any{"column": "customer", "as": "who"}}}, false, order(1, "x", "0"))
	if v, _ := looked[0].Get("who"); err != nil || v != "Grace" {
		t.Errorf("lookup: %v, %v", v, err)
	}
}
