package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/sqldb"
)

// db.query reads a few rows of a query into the workflow, e.g. for a condition or for
// forEach. Output: rows (a list of objects, one per row), count, first (the first row or
// null) and value (the first column of the first row, e.g. of SELECT COUNT(*)). Larger
// results belong into a pipeline with db.table-input.
var queryDefinition = action.Definition{
	Type:        "db.query",
	Category:    action.CategoryDatabase,
	Label:       "Query SQL",
	Description: "Reads the rows of an SQL query into the workflow; output: rows, count, first and value",
	Example:     "connection: app-db\nsql: SELECT COUNT(*) AS open FROM orders WHERE status = @status\nparams:\n  status: open\n",
	Config:      queryAction{},
	New:         newQuery,
}

// defaultMaxRows limits the rows a query may return (held in memory and in the result).
const defaultMaxRows = 1000

type queryAction struct {
	Connection string            `json:"connection" doc:"Connection of the database (a file in connections/)" schema:"required,connection=sqlite|sqlserver|postgres,example=app-db"`
	SQL        string            `json:"sql" doc:"The query; values as @name with the value under params, never written into the query" schema:"required,format=runnel-sql,example=SELECT COUNT(*) FROM orders"`
	Params     map[string]string `json:"params" doc:"Values of the parameters @name of the query; may use ${…} and {{ … }}"`
	MaxRows    string            `json:"maxRows" doc:"Most rows the query may return; more fail the action (for large results use a pipeline with db.table-input)" schema:"default=1000"`
	Timeout    string            `json:"timeout" doc:"Longest time the query may take" schema:"format=runnel-duration,default=1m"`
}

func newQuery(with map[string]any) (action.Action, error) {
	a := &queryAction{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	if _, err := a.timeout(); err != nil {
		return nil, err
	}
	if _, err := sqldb.Args(a.SQL, a.Params); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *queryAction) timeout() (time.Duration, error) {
	if a.Timeout == "" {
		return time.Minute, nil
	}
	return sqldb.Timeout(a.Timeout)
}

// maxRows is checked when the action runs: it may be a variable before.
func (a *queryAction) maxRows() (int, error) {
	if a.MaxRows == "" {
		return defaultMaxRows, nil
	}
	n, err := strconv.Atoi(a.MaxRows)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("maxRows %q is not a whole number of at least 1", a.MaxRows)
	}
	return n, nil
}

func (a *queryAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	most, err := a.maxRows()
	if err != nil {
		return action.Result{}, err
	}
	if ec.Connection == nil {
		return action.Result{}, connection.ErrNotAvailable
	}
	conn, err := ec.Connection(ctx, a.Connection)
	if err != nil {
		return action.Result{}, err
	}
	db, err := sqldb.Open(ctx, connections.Builtin(), conn)
	if err != nil {
		return action.Result{}, err
	}
	stmt, args, err := sqldb.Statement(conn, a.SQL, a.Params)
	if err != nil {
		return action.Result{}, err
	}
	timeout, _ := a.timeout()
	qctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	rows, err := a.read(qctx, db, stmt, args, most)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		return action.Result{}, ctx.Err()
	case errors.Is(qctx.Err(), context.DeadlineExceeded):
		return action.Result{}, fmt.Errorf("the query took longer than %s", timeout)
	default:
		return action.Result{}, err
	}
	list := make([]any, len(rows))
	for i, r := range rows {
		list[i] = r.values
	}
	out := map[string]any{"rows": list, "count": int64(len(rows)), "first": nil, "value": nil}
	if len(rows) > 0 {
		out["first"] = rows[0].values
		out["value"] = rows[0].first
	}
	ec.Log(slog.LevelInfo, fmt.Sprintf("read %d %s", len(rows), plural(int64(len(rows)))))
	return action.Result{Output: out}, nil
}

// queryRow is a row as an object, and the value of its first column.
type queryRow struct {
	values map[string]any
	first  any
}

func (a *queryAction) read(ctx context.Context, db *sql.DB, stmt string, args []any, most int) ([]queryRow, error) {
	rows, err := db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("connection %s: %w", a.Connection, err)
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name()
		if slices.Contains(names[:i], names[i]) {
			return nil, fmt.Errorf("the query returns the column %q twice; give one of them another name with AS", names[i])
		}
	}
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	var out []queryRow
	for rows.Next() {
		if len(out) == most {
			return nil, fmt.Errorf("the query returns more than %d rows (maxRows); read larger results in a pipeline with db.table-input", most)
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := queryRow{values: make(map[string]any, len(cols))}
		for i, c := range cols {
			v := sqldb.Value(values[i], c.DatabaseTypeName())
			row.values[names[i]] = v
			if i == 0 {
				row.first = v
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("connection %s: %w", a.Connection, err)
	}
	return out, nil
}
