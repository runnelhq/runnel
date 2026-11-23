package db

import (
	"container/list"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/sqldb"
	"github.com/runnelhq/runnel/internal/transform"
)

// db.lookup adds the columns of the matching row of a table to each record, like a left
// join: one query per key, with a cache of the last results (S23a).
var lookupDefinition = transform.Definition{
	Type:         "db.lookup",
	Category:     transform.CategoryCombine,
	Label:        "Database Lookup",
	Description:  "Adds the columns of the matching row of a database table to each record",
	Example:      "connection: app-db\ntable: customers\nkeys:\n  - { field: customerId, column: id }\nreturn:\n  - { column: name, as: customerName }\n",
	Config:       lookup{},
	New:          newLookup,
	RecordErrors: true,
	Parallel:     true,
}

type lookup struct {
	Connection string         `json:"connection" doc:"Connection of the database (a file in connections/)" schema:"required,connection=sqlite|sqlserver|postgres,example=app-db"`
	Table      string         `json:"table" doc:"Table to look in: table or schema.table" schema:"required,example=customers"`
	Keys       []lookupKey    `json:"keys" doc:"Fields of the record and the columns they must be equal to" schema:"required"`
	Return     []lookupReturn `json:"return" doc:"Columns of the matching row added to the record" schema:"required"`
	NotFound   string         `json:"notFound" doc:"Records without a matching row: keep them with the defaults, drop them, or treat them as errors (error link)" schema:"enum=keep|drop|error,default=keep"`
	Multiple   string         `json:"multiple" doc:"Several matching rows: take the first the database returns, or treat the record as an error" schema:"enum=first|error,default=first"`
	Cache      *int           `json:"cache" doc:"Results kept in memory, by key; 0: ask the database for every record" schema:"default=1000"`
	Timeout    string         `json:"timeout" doc:"Longest time one query may take" schema:"format=runnel-duration,default=30s"`
}

type lookupKey struct {
	Field  string `json:"field" doc:"Field of the record" schema:"required,format=runnel-field,example=customerId"`
	Column string `json:"column" doc:"Column of the table; empty: the name of the field" schema:"example=id"`
}

type lookupReturn struct {
	Column  string `json:"column" doc:"Column of the table" schema:"required,example=name"`
	As      string `json:"as" doc:"Field for the value; empty: the name of the column"`
	Default any    `json:"default" doc:"Value when no row matches; missing: an empty value"`
}

func newLookup(with map[string]any) (transform.Transform, error) {
	t := &lookup{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	if err := sqldb.CheckTable(t.Table); err != nil {
		return nil, err
	}
	if len(t.Keys) == 0 {
		return nil, errors.New("keys: name at least one field and column to compare")
	}
	if len(t.Return) == 0 {
		return nil, errors.New("return: name at least one column to add")
	}
	for i := range t.Keys {
		if t.Keys[i].Column == "" {
			t.Keys[i].Column = t.Keys[i].Field
		}
		if err := sqldb.CheckColumn(t.Keys[i].Column); err != nil {
			return nil, fmt.Errorf("keys[%d]: %w", i, err)
		}
	}
	for i := range t.Return {
		r := &t.Return[i]
		if err := sqldb.CheckColumn(r.Column); err != nil {
			return nil, fmt.Errorf("return[%d]: %w", i, err)
		}
		if r.As == "" {
			r.As = r.Column
		}
		v, err := record.Normalize(r.Default)
		if err != nil {
			return nil, fmt.Errorf("return[%d] (%s): default: %w", i, r.Column, err)
		}
		r.Default = v
	}
	switch t.NotFound {
	case "", "keep", "drop", "error":
	default:
		return nil, fmt.Errorf("invalid notFound %q (expected keep, drop or error)", t.NotFound)
	}
	switch t.Multiple {
	case "", "first", "error":
	default:
		return nil, fmt.Errorf("invalid multiple %q (expected first or error)", t.Multiple)
	}
	if t.Cache != nil && *t.Cache < 0 {
		return nil, errors.New("cache must not be negative")
	}
	_, err := sqldb.Timeout(t.Timeout)
	return t, err
}

// Init checks the key fields and adds the returned fields; their type is only known when
// the query runs, unless a default gives it.
func (t *lookup) Init(ic *transform.InitContext) (record.Schema, error) {
	for i, k := range t.Keys {
		ic.Use(k.Field, fmt.Sprintf("keys[%d].field", i))
	}
	s := ic.In
	for _, r := range t.Return {
		s = s.With(r.As, record.TypeOf(r.Default))
	}
	return s, nil
}

func (t *lookup) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	conn, err := out.Connection(ctx, t.Connection)
	if err != nil {
		return err
	}
	db, err := sqldb.Open(ctx, connections.Builtin(), conn)
	if err != nil {
		return err
	}
	keys := make([]string, len(t.Keys))
	for i, k := range t.Keys {
		keys[i] = k.Column
	}
	cols := make([]string, len(t.Return))
	for i, r := range t.Return {
		cols[i] = r.Column
	}
	stmt, err := db.PrepareContext(ctx, sqldb.DialectOf(conn.Type).Select(t.Table, cols, keys))
	if err != nil {
		return fmt.Errorf("%s: %w", t.Table, err)
	}
	defer func() { _ = stmt.Close() }()
	size := 1000
	if t.Cache != nil {
		size = *t.Cache
	}
	cache := newLRU(size)
	timeout, _ := sqldb.Timeout(t.Timeout)
	var read, found, queries int
	for b := range in {
		for _, r := range b {
			read++
			args := make([]any, len(t.Keys))
			null := false
			for i, k := range t.Keys {
				v, _ := r.Get(k.Field)
				a, err := sqldb.Arg(v)
				if err != nil {
					return fmt.Errorf("record %d, field %q: %w", read, k.Field, err)
				}
				args[i], null = a, null || v == nil
			}
			var res lookupResult
			if !null { // = NULL matches no row
				id := cacheKey(args)
				var ok bool
				if res, ok = cache.get(id); !ok {
					queries++
					if res, err = t.query(ctx, stmt, timeout, args); err != nil {
						return err
					}
					cache.put(id, res)
				}
			}
			var problem string
			switch {
			case res.values == nil && t.NotFound == "drop":
				continue
			case res.values == nil && t.NotFound == "error":
				problem = "no row in " + t.Table + " matches"
			case res.multiple && t.Multiple == "error":
				problem = "several rows in " + t.Table + " match"
			}
			if problem != "" {
				if err := out.Fail(r, t.Keys[0].Field, fmt.Errorf("record %d: %s %s", read, problem, t.describe(r))); err != nil {
					return err
				}
				continue
			}
			if res.values != nil {
				found++
			}
			for i, rt := range t.Return {
				v := rt.Default
				if res.values != nil {
					v = res.values[i]
				}
				r.Set(rt.As, v)
			}
			if err := out.Emit(r); err != nil {
				return err
			}
		}
	}
	out.Logf(slog.LevelInfo, "looked up %d %s in %s: %d found, %d %s", read, records(read), t.Table, found, queries, plural(queries, "query", "queries"))
	return ctx.Err()
}

// describe names the key values of a record for messages, e.g. (customerId = 7).
func (t *lookup) describe(r record.Record) string {
	parts := make([]string, len(t.Keys))
	for i, k := range t.Keys {
		v, _ := r.Get(k.Field)
		parts[i] = fmt.Sprintf("%s = %v", k.Field, v)
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// lookupResult is the answer for one key: the values of the first row (nil: none) and
// whether there were more.
type lookupResult struct {
	values   []any
	multiple bool
}

func (t *lookup) query(ctx context.Context, stmt *sql.Stmt, timeout time.Duration, args []any) (lookupResult, error) {
	qctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := t.read(qctx, stmt, args)
	switch {
	case err == nil:
		return res, nil
	case ctx.Err() != nil:
		return res, ctx.Err()
	case errors.Is(qctx.Err(), context.DeadlineExceeded):
		return res, fmt.Errorf("looking up in %s took longer than %s", t.Table, timeout)
	}
	return res, fmt.Errorf("%s: %w", t.Table, err)
}

func (t *lookup) read(ctx context.Context, stmt *sql.Stmt, args []any) (lookupResult, error) {
	var res lookupResult
	rows, err := stmt.QueryContext(ctx, args...)
	if err != nil {
		return res, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return res, rows.Err()
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		return res, err
	}
	values := make([]any, len(types))
	ptrs := make([]any, len(types))
	for i := range values {
		ptrs[i] = &values[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return res, err
	}
	for i, c := range types {
		values[i] = sqldb.Value(values[i], c.DatabaseTypeName())
	}
	res.values = values
	res.multiple = rows.Next()
	return res, rows.Err()
}

// cacheKey identifies the values of the keys: type and value, so that 1 and "1" differ.
func cacheKey(args []any) string {
	var b strings.Builder
	for _, a := range args {
		fmt.Fprintf(&b, "%T\x00%v\x00", a, a)
	}
	return b.String()
}

// lru keeps the results of the last size keys.
type lru struct {
	size  int
	order *list.List // front: used last
	items map[string]*list.Element
}

type lruEntry struct {
	key string
	res lookupResult
}

func newLRU(size int) *lru {
	return &lru{size: size, order: list.New(), items: map[string]*list.Element{}}
}

func (c *lru) get(key string) (lookupResult, bool) {
	e, ok := c.items[key]
	if !ok {
		return lookupResult{}, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*lruEntry).res, true
}

func (c *lru) put(key string, res lookupResult) {
	if c.size == 0 {
		return
	}
	if c.order.Len() >= c.size {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.items, last.Value.(*lruEntry).key)
	}
	c.items[key] = c.order.PushFront(&lruEntry{key: key, res: res})
}

func records(n int) string { return plural(n, "record", "records") }

func plural(n int, one, more string) string {
	if n == 1 {
		return one
	}
	return more
}
