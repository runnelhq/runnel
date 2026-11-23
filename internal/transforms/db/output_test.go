package db

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/sqldb"
	"github.com/runnelhq/runnel/internal/transform"
)

// write runs a writing transform on a connection; it returns the records passed on, those
// sent to the error link (if errorLink), the log and the error.
func write(t testing.TB, def transform.Definition, conn connection.Resolved, with map[string]any, errorLink bool, input ...record.Record) (passed, failed []record.Record, logs []string, err error) {
	t.Helper()
	with["connection"] = "test"
	tr, err := def.New(with)
	if err != nil {
		return nil, nil, nil, err
	}
	in := make(chan transform.Batch, len(input))
	for _, r := range input {
		in <- transform.Batch{r}
	}
	close(in)
	o := transform.NewOutput("out", func(r record.Record) error { passed = append(passed, r); return nil },
		func(_ slog.Level, m string) { logs = append(logs, m) }).
		WithConnections(func(context.Context, string) (connection.Resolved, error) { return conn, nil })
	if errorLink {
		o = o.WithErrorLinks(func(r record.Record) error { failed = append(failed, r); return nil })
	}
	err = tr.Process(context.Background(), in, o)
	return passed, failed, logs, err
}

func order(id int64, customer string, amount string) record.Record {
	return record.New(record.Field{Name: "id", Value: id}, record.Field{Name: "customer", Value: customer},
		record.Field{Name: "amount", Value: decimal.RequireFromString(amount)})
}

func count(t *testing.T, conn connection.Resolved, query string) int {
	t.Helper()
	db, err := sqldb.Open(context.Background(), connections.Builtin(), conn)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

const ordersTable = "CREATE TABLE orders (id INTEGER PRIMARY KEY, customer TEXT, amount DECIMAL(10,2), day DATE, tags TEXT)"

// db.table-output inserts the records in transactions of commitSize and passes them on;
// fields chooses the columns, the others stay NULL; dates and lists are written.
func TestTableOutput(t *testing.T) {
	conn := resolved(testDB(t, ordersTable))
	var input []record.Record
	for i := range int64(25) {
		r := order(i+1, "c", "1.50")
		r.Set("day", record.DateOf(time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)))
		r.Set("tags", []any{"a", "b"})
		r.Set("_source", "skipped: metadata")
		input = append(input, r)
	}
	passed, _, logs, err := write(t, tableOutputDefinition, conn, map[string]any{"table": "orders", "commitSize": 10}, false, input...)
	if err != nil || len(passed) != 25 || logs[len(logs)-1] != "wrote 25 rows to orders" {
		t.Fatalf("%d passed, %v, %v", len(passed), logs, err)
	}
	recs, err := readResolved(context.Background(), conn, map[string]any{"sql": "SELECT amount, day, tags FROM orders WHERE id = 25"})
	if err != nil || len(recs) != 1 {
		t.Fatal(recs, err)
	}
	if v, _ := recs[0].Get("tags"); v != `["a","b"]` {
		t.Errorf("tags = %#v", v)
	}
	if v, _ := recs[0].Get("amount"); !v.(decimal.Decimal).Equal(decimal.RequireFromString("1.5")) {
		t.Errorf("amount = %#v", v)
	}

	_, _, _, err = write(t, tableOutputDefinition, conn, map[string]any{"table": "orders", "fields": []any{"id", "customer"}}, false, order(100, "Ada", "9"))
	if err != nil || count(t, conn, "SELECT count(*) FROM orders WHERE id = 100 AND amount IS NULL") != 1 {
		t.Errorf("fields: %v", err)
	}
}

// A failing transaction is rolled back. With an error link, only the rows that fail go
// there; without, the pipeline stops and the earlier transactions stay.
func TestTableOutputErrors(t *testing.T) {
	conn := resolved(testDB(t, ordersTable, "INSERT INTO orders (id, customer) VALUES (3, 'there')"))
	input := []record.Record{order(1, "a", "1"), order(2, "b", "1"), order(3, "dup", "1"), order(4, "d", "1")}
	passed, failed, logs, err := write(t, tableOutputDefinition, conn, map[string]any{"table": "orders", "commitSize": 2}, true, input...)
	if err != nil || len(passed) != 3 || len(failed) != 1 || !strings.Contains(logs[len(logs)-1], "wrote 3 rows to orders, 1 failed") {
		t.Fatalf("%d passed, %d failed, %v, %v", len(passed), len(failed), logs, err)
	}
	if v, _ := failed[0].Get("customer"); v != "dup" {
		t.Errorf("failed %v", failed[0])
	}

	conn = resolved(testDB(t, ordersTable, "INSERT INTO orders (id, customer) VALUES (3, 'there')"))
	_, _, _, err = write(t, tableOutputDefinition, conn, map[string]any{"table": "orders", "commitSize": 2}, false, input...)
	if err == nil || !strings.Contains(err.Error(), "orders: constraint failed: UNIQUE constraint failed: orders.id") {
		t.Errorf("without error link: %v", err)
	}
	if n := count(t, conn, "SELECT count(*) FROM orders"); n != 3 { // the first transaction (1, 2) and the row there before
		t.Errorf("%d rows after the failure, want 3 (rolled back: 4)", n)
	}

	for with, want := range map[string]map[string]any{
		`invalid table "orders; DROP TABLE x"`: {"table": "orders; DROP TABLE x"},
		`invalid column name "a b"`:            {"table": "orders", "fields": []any{"a b"}},
		"commitSize must not be negative":      {"table": "orders", "commitSize": -1},
	} {
		if _, _, _, err := write(t, tableOutputDefinition, conn, want, false); err == nil || !strings.Contains(err.Error(), with) {
			t.Errorf("%v: %v, want %q", want, err, with)
		}
	}
}

// db.upsert inserts new rows and updates existing ones by their keys; update limits the
// columns that change.
func TestUpsert(t *testing.T) {
	conn := resolved(testDB(t, ordersTable, "INSERT INTO orders (id, customer, amount) VALUES (1, 'old', 1)"))
	passed, _, _, err := write(t, upsertDefinition, conn, map[string]any{"table": "orders", "keys": []any{"id"}}, false,
		order(1, "Ada", "10"), order(2, "Grace", "20"))
	if err != nil || len(passed) != 2 {
		t.Fatalf("%d, %v", len(passed), err)
	}
	if count(t, conn, "SELECT count(*) FROM orders WHERE (id = 1 AND customer = 'Ada' AND amount = 10) OR (id = 2 AND customer = 'Grace')") != 2 {
		t.Error("rows not inserted or updated")
	}
	_, _, _, err = write(t, upsertDefinition, conn, map[string]any{"table": "orders", "keys": []any{"id"}, "update": []any{"amount"}}, false, order(1, "Changed", "11"))
	if err != nil || count(t, conn, "SELECT count(*) FROM orders WHERE id = 1 AND customer = 'Ada' AND amount = 11") != 1 {
		t.Errorf("update only amount: %v", err)
	}
	// Without a unique index on the keys, SQLite refuses ON CONFLICT.
	_, _, _, err = write(t, upsertDefinition, conn, map[string]any{"table": "orders", "keys": []any{"customer"}}, false, order(9, "Ada", "1"))
	if err == nil || !strings.Contains(err.Error(), "ON CONFLICT clause does not match any PRIMARY KEY or UNIQUE constraint") {
		t.Errorf("no unique index: %v", err)
	}
	for want, with := range map[string]map[string]any{
		`missing required field "keys"`:            {"table": "orders"},
		"keys: name at least one field":            {"table": "orders", "keys": []any{}},
		"amount is not one of the fields to write": {"table": "orders", "keys": []any{"id"}, "fields": []any{"id"}, "update": []any{"amount"}},
	} {
		with["connection"] = "x"
		if _, err := upsertDefinition.New(with); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: %v", with, err)
		}
	}
}

// Init reports fields that do not come in.
func TestOutputInit(t *testing.T) {
	tr, err := upsertDefinition.New(map[string]any{"connection": "x", "table": "t", "keys": []any{"idd"}})
	if err != nil {
		t.Fatal(err)
	}
	ic := transform.NewInitContext(record.Schema{}.With("id", record.Int).With("name", record.String))
	if _, err := tr.(interface {
		Init(*transform.InitContext) (record.Schema, error)
	}).Init(ic); err != nil {
		t.Fatal(err)
	}
	if p := ic.Problems(); len(p) != 1 || p[0].Path != "keys[0]" || !strings.Contains(p[0].Message, `did you mean "id"`) {
		t.Errorf("%+v", p)
	}
}

// The SQL of the dialects.
func TestDialects(t *testing.T) {
	cols, keys, update := []string{"id", "name"}, []string{"id"}, []string{"name"}
	for typ, want := range map[string]string{
		connections.TypeSQLite:   `INSERT INTO "sales"."t" ("id", "name") VALUES (?, ?) ON CONFLICT ("id") DO UPDATE SET "name" = excluded."name"`,
		connections.TypePostgres: `INSERT INTO "sales"."t" ("id", "name") VALUES ($1, $2) ON CONFLICT ("id") DO UPDATE SET "name" = excluded."name"`,
	} {
		if got := sqldb.DialectOf(typ).Upsert("sales.t", cols, keys, update); got != want {
			t.Errorf("%s:\n%s\nwant\n%s", typ, got, want)
		}
	}
	ms := sqldb.DialectOf(connections.TypeSQLServer)
	if got := ms.Update("sales.t", keys, update); got != "UPDATE [sales].[t] WITH (UPDLOCK, SERIALIZABLE) SET [name] = @p1 WHERE [id] = @p2" || !ms.SplitUpsert() {
		t.Errorf("sql server update: %s", got)
	}
	if got := ms.Insert("t", cols); got != "INSERT INTO [t] ([id], [name]) VALUES (@p1, @p2)" {
		t.Errorf("sql server insert: %s", got)
	}
}

// BenchmarkTableOutput writes 100,000 rows with three columns in transactions of 1,000.
func BenchmarkTableOutput(b *testing.B) {
	input := make([]record.Record, 100000)
	for i := range input {
		input[i] = order(int64(i), "customer", "12.50")
	}
	b.ReportAllocs()
	for b.Loop() {
		conn := resolved(testDB(b, ordersTable))
		if _, _, _, err := write(b, tableOutputDefinition, conn, map[string]any{"table": "orders"}, false, input...); err != nil {
			b.Fatal(err)
		}
	}
}

// Outside a project there are no connections.
func TestOutputNoConnection(t *testing.T) {
	tr, err := tableOutputDefinition.New(map[string]any{"connection": "x", "table": "t"})
	if err != nil {
		t.Fatal(err)
	}
	in := make(chan transform.Batch, 1)
	in <- transform.Batch{order(1, "a", "1")}
	close(in)
	err = tr.Process(context.Background(), in, transform.NewOutput("out", func(record.Record) error { return nil }, nil))
	if err == nil || !errors.Is(err, connection.ErrNotAvailable) {
		t.Errorf("%v", err)
	}
}
