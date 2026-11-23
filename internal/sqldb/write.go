package sqldb

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/record"
)

// identifier is a table or column name that may be quoted: letters, digits and _, not
// starting with a digit; a table may name its schema (sales.orders).
var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// CheckTable reports a table name that is not schema.table or table of plain names
// (ADR 0022: names from variables only through checked fields).
func CheckTable(name string) error {
	parts := strings.Split(name, ".")
	if len(parts) > 2 {
		return fmt.Errorf("invalid table %q (expected table or schema.table)", name)
	}
	for _, p := range parts {
		if !identifier.MatchString(p) {
			return fmt.Errorf("invalid table %q (letters, digits and _, not starting with a digit)", name)
		}
	}
	return nil
}

// CheckColumn reports a column name that cannot be written as it is.
func CheckColumn(name string) error {
	if !identifier.MatchString(name) {
		return fmt.Errorf("invalid column name %q (letters, digits and _, not starting with a digit)", name)
	}
	return nil
}

// Dialect writes the statements of a database type.
type Dialect struct{ typ string }

// DialectOf returns the dialect of a connection type.
func DialectOf(typ string) Dialect { return Dialect{typ: typ} }

// Quote writes a checked name (table, schema.table or column) quoted for the database.
func (d Dialect) Quote(name string) string {
	parts := strings.Split(name, ".")
	for i, p := range parts {
		if d.typ == connections.TypeSQLServer {
			parts[i] = "[" + p + "]"
		} else {
			parts[i] = `"` + p + `"`
		}
	}
	return strings.Join(parts, ".")
}

// Placeholder returns the n-th (from 1) positional parameter.
func (d Dialect) Placeholder(n int) string {
	switch d.typ {
	case connections.TypePostgres:
		return "$" + strconv.Itoa(n)
	case connections.TypeSQLServer:
		return "@p" + strconv.Itoa(n)
	}
	return "?"
}

func (d Dialect) list(names []string, f func(i int, name string) string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = f(i, n)
	}
	return strings.Join(parts, ", ")
}

// Insert returns INSERT INTO table (columns) VALUES (…), the values in the order of columns.
func (d Dialect) Insert(table string, columns []string) string {
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", d.Quote(table),
		d.list(columns, func(_ int, c string) string { return d.Quote(c) }),
		d.list(columns, func(i int, _ string) string { return d.Placeholder(i + 1) }))
}

// Upsert returns the statement that inserts a row or, if a row with the same keys
// exists, updates the columns update (none: leaves the row as it is); the values in the
// order of columns. SQLite and PostgreSQL use INSERT … ON CONFLICT and need a unique index
// on the keys. SQL Server has no such statement: Update and Insert are separate there
// (SplitUpsert).
func (d Dialect) Upsert(table string, columns, keys, update []string) string {
	stmt := d.Insert(table, columns) + " ON CONFLICT (" + d.list(keys, func(_ int, k string) string { return d.Quote(k) }) + ") "
	if len(update) == 0 {
		return stmt + "DO NOTHING"
	}
	return stmt + "DO UPDATE SET " + d.list(update, func(_ int, c string) string { return d.Quote(c) + " = excluded." + d.Quote(c) })
}

// SplitUpsert reports whether an upsert runs as UPDATE and, if no row was changed,
// INSERT (SQL Server) instead of one statement.
func (d Dialect) SplitUpsert() bool { return d.typ == connections.TypeSQLServer }

// Update returns UPDATE table SET update WHERE keys for SplitUpsert; the values are those
// of update, then those of keys. The row is locked until the end of the transaction, so
// that a parallel run cannot insert it in between (UPDLOCK, SERIALIZABLE).
func (d Dialect) Update(table string, keys, update []string) string {
	set := update
	if len(set) == 0 {
		set = keys[:1] // nothing to change: a harmless SET to find out whether the row exists
	}
	n := len(set)
	return fmt.Sprintf("UPDATE %s WITH (UPDLOCK, SERIALIZABLE) SET %s WHERE %s", d.Quote(table),
		d.list(set, func(i int, c string) string { return d.Quote(c) + " = " + d.Placeholder(i+1) }),
		strings.Join(func() []string {
			out := make([]string, len(keys))
			for i, k := range keys {
				out[i] = d.Quote(k) + " = " + d.Placeholder(n+i+1)
			}
			return out
		}(), " AND "))
}

// Arg converts a value of a record (ADR 0007) into a value the drivers accept: dates as
// time, lists and objects as JSON text.
func Arg(v any) (any, error) {
	switch v := v.(type) {
	case record.Date:
		return v.In(time.UTC), nil
	case map[string]any, []any:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return string(data), nil
	}
	return v, nil
}

// Select returns SELECT columns FROM table WHERE keys, the values those of keys (for
// db.lookup); each key is compared with =.
func (d Dialect) Select(table string, columns, keys []string) string {
	where := make([]string, len(keys))
	for i, k := range keys {
		where[i] = d.Quote(k) + " = " + d.Placeholder(i+1)
	}
	return fmt.Sprintf("SELECT %s FROM %s WHERE %s",
		d.list(columns, func(_ int, c string) string { return d.Quote(c) }), d.Quote(table), strings.Join(where, " AND "))
}
