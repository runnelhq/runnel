package db

import (
	"strconv"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/sqldb"
	"github.com/runnelhq/runnel/internal/transform"
)

const customersTable = "CREATE TABLE customers (id INTEGER, region TEXT, name TEXT, limit_ DECIMAL(10,2))"

func customers(t testing.TB) connection.Resolved {
	return resolved(testDB(t, customersTable,
		"INSERT INTO customers VALUES (1, 'n', 'Ada', 100), (2, 'n', 'Grace', 200), (2, 's', 'Grace South', 50), (3, 'n', 'Twin A', 1), (3, 'n', 'Twin B', 2)"))
}

func customer(id any) record.Record {
	return record.New(record.Field{Name: "customerId", Value: id}, record.Field{Name: "amount", Value: int64(10)})
}

// db.lookup adds the columns of the matching row; records without one keep the defaults.
func TestLookup(t *testing.T) {
	conn := customers(t)
	with := map[string]any{"table": "customers", "keys": []any{map[string]any{"field": "customerId", "column": "id"}},
		"return": []any{map[string]any{"column": "name", "as": "customerName", "default": "unknown"}, map[string]any{"column": "limit_"}}}
	passed, _, logs, err := write(t, lookupDefinition, conn, with, false, customer(int64(1)), customer(int64(9)), customer(nil), customer(int64(1)))
	if err != nil || len(passed) != 4 {
		t.Fatalf("%d passed, %v", len(passed), err)
	}
	for i, want := range []string{"Ada", "unknown", "unknown", "Ada"} {
		if v, _ := passed[i].Get("customerName"); v != want {
			t.Errorf("record %d: customerName = %#v, want %q", i, v, want)
		}
	}
	if v, _ := passed[0].Get("limit_"); !v.(decimal.Decimal).Equal(decimal.NewFromInt(100)) {
		t.Errorf("limit_ = %#v", v)
	}
	if v, ok := passed[1].Get("limit_"); !ok || v != nil {
		t.Errorf("limit_ without a row = %#v, %v; want an empty value", v, ok)
	}
	// The second Ada comes from the cache; an empty key asks nothing.
	if want := "looked up 4 records in customers: 2 found, 2 queries"; logs[len(logs)-1] != want {
		t.Errorf("log %q, want %q", logs[len(logs)-1], want)
	}
	with["cache"] = 0
	if _, _, logs, _ := write(t, lookupDefinition, conn, with, false, customer(int64(1)), customer(int64(1))); !strings.HasSuffix(logs[len(logs)-1], "2 queries") {
		t.Errorf("without cache: %v", logs)
	}
}

// Several keys, several matching rows, drop and error.
func TestLookupMatches(t *testing.T) {
	conn := customers(t)
	keys := []any{map[string]any{"field": "customerId", "column": "id"}, map[string]any{"field": "region"}}
	ret := []any{map[string]any{"column": "name"}}
	south := customer(int64(2))
	south.Set("region", "s")
	passed, _, _, err := write(t, lookupDefinition, conn, map[string]any{"table": "customers", "keys": keys, "return": ret}, false, south)
	if v, _ := passed[0].Get("name"); err != nil || v != "Grace South" {
		t.Errorf("two keys: %v, %v", v, err)
	}

	one := []any{map[string]any{"field": "customerId", "column": "id"}}
	passed, failed, _, err := write(t, lookupDefinition, conn, map[string]any{"table": "customers", "keys": one, "return": ret, "notFound": "drop", "multiple": "error"}, true,
		customer(int64(1)), customer(int64(3)), customer(int64(9)))
	if err != nil || len(passed) != 1 || len(failed) != 1 {
		t.Fatalf("%d passed, %d failed, %v", len(passed), len(failed), err)
	}
	if v, _ := failed[0].Get(transform.ErrorField); v != "record 2: several rows in customers match (customerId = 3)" {
		t.Errorf("error %v", v)
	}

	_, _, _, err = write(t, lookupDefinition, conn, map[string]any{"table": "customers", "keys": one, "return": ret, "notFound": "error"}, false, customer(int64(1)), customer(int64(9)))
	if err == nil || err.Error() != "record 2: no row in customers matches (customerId = 9)" {
		t.Errorf("not found without error link: %v", err)
	}
	// first: one of the rows, without error.
	if passed, _, _, err := write(t, lookupDefinition, conn, map[string]any{"table": "customers", "keys": one, "return": ret}, false, customer(int64(3))); err != nil || len(passed) != 1 {
		t.Errorf("first: %v", err)
	}
}

func TestLookupErrors(t *testing.T) {
	conn := customers(t)
	one := []any{map[string]any{"field": "customerId", "column": "id"}}
	ret := []any{map[string]any{"column": "name"}}
	_, _, _, err := write(t, lookupDefinition, conn, map[string]any{"table": "nothere", "keys": one, "return": ret}, false, customer(int64(1)))
	if err == nil || !strings.Contains(err.Error(), "nothere: SQL logic error: no such table: nothere") {
		t.Errorf("unknown table: %v", err)
	}
	for want, with := range map[string]map[string]any{
		`invalid table "a;b"`:                                    {"table": "a;b", "keys": one, "return": ret},
		`keys[0]: invalid column name "a b"`:                     {"table": "t", "keys": []any{map[string]any{"field": "a b"}}, "return": ret},
		`return[0]: invalid column name "x-y"`:                   {"table": "t", "keys": one, "return": []any{map[string]any{"column": "x-y"}}},
		"keys: name at least one field":                          {"table": "t", "keys": []any{}, "return": ret},
		"return: name at least one column":                       {"table": "t", "keys": one, "return": []any{}},
		"cache must not be negative":                             {"table": "t", "keys": one, "return": ret, "cache": -1},
		`invalid notFound "skip" (expected keep, drop or error)`: {"table": "t", "keys": one, "return": ret, "notFound": "skip"},
		`invalid multiple "all"`:                                 {"table": "t", "keys": one, "return": ret, "multiple": "all"},
	} {
		with["connection"] = "x"
		if _, err := lookupDefinition.New(with); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: %v, want %q", with, err, want)
		}
	}
}

// Init reports unknown key fields and adds the returned fields, typed by their default.
func TestLookupInit(t *testing.T) {
	tr, err := lookupDefinition.New(map[string]any{"connection": "x", "table": "t", "keys": []any{map[string]any{"field": "custId"}},
		"return": []any{map[string]any{"column": "name"}, map[string]any{"column": "n", "default": 0}}})
	if err != nil {
		t.Fatal(err)
	}
	ic := transform.NewInitContext(record.Schema{}.With("customerId", record.Int))
	s, _ := tr.(interface {
		Init(*transform.InitContext) (record.Schema, error)
	}).Init(ic)
	if p := ic.Problems(); len(p) != 1 || p[0].Path != "keys[0].field" {
		t.Errorf("%+v", p)
	}
	if f, _ := s.Field("n"); f.Type != record.Int {
		t.Errorf("n: %v", f.Type)
	}
	if f, ok := s.Field("name"); !ok || f.Type != record.Unknown {
		t.Errorf("name: %v %v", f, ok)
	}
}

func TestLRU(t *testing.T) {
	c := newLRU(2)
	c.put("a", lookupResult{values: []any{1}})
	c.put("b", lookupResult{})
	c.get("a")
	c.put("c", lookupResult{})
	if _, ok := c.get("b"); ok {
		t.Error("b should have been dropped, it was used last before a")
	}
	if _, ok := c.get("a"); !ok {
		t.Error("a was dropped")
	}
}

func TestSelect(t *testing.T) {
	if got := sqldb.DialectOf("sqlserver").Select("s.t", []string{"a", "b"}, []string{"k", "l"}); got != "SELECT [a], [b] FROM [s].[t] WHERE [k] = @p1 AND [l] = @p2" {
		t.Error(got)
	}
}

// BenchmarkLookup looks up 100,000 records among 1,000 keys with the default cache.
func BenchmarkLookup(b *testing.B) {
	stmts := []string{"CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT)"}
	var values []string
	for i := range 1000 {
		values = append(values, "("+strconv.Itoa(i)+", 'name')")
	}
	stmts = append(stmts, "INSERT INTO customers VALUES "+strings.Join(values, ","))
	conn := resolved(testDB(b, stmts...))
	with := map[string]any{"table": "customers", "keys": []any{map[string]any{"field": "customerId", "column": "id"}}, "return": []any{map[string]any{"column": "name"}}}
	input := make([]record.Record, 100000)
	for i := range input {
		input[i] = customer(int64(i % 1000))
	}
	b.ReportAllocs()
	for b.Loop() {
		recs := make([]record.Record, len(input))
		for i, r := range input {
			recs[i] = r.Clone()
		}
		if _, _, _, err := write(b, lookupDefinition, conn, with, false, recs...); err != nil {
			b.Fatal(err)
		}
	}
}
