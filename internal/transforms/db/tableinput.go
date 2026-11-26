package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/sqldb"
	"github.com/runnelhq/runnel/internal/transform"
)

// db.table-input reads the rows of a query as records: one field per column, in the
// order of the columns, with the types of ADR 0007 (sqldb.Value).
var tableInputDefinition = transform.Definition{
	Type:        "db.table-input",
	Category:    transform.CategoryInput,
	Label:       "Table Input",
	Description: "Reads the rows of an SQL query as records, one field per column",
	Example:     "connection: app-db\nsql: SELECT id, name, amount FROM orders WHERE status = @status\nparams:\n  status: open\n",
	Config:      tableInput{},
	New:         newTableInput,
	Reference: action.Reference{
		Records: "Creates one record per row of the query, one field per column in their order with the type the database returns, and passes it to all data links; it has no incoming links.",
		Errors:  "Stops the pipeline when the connection cannot be opened, a parameter @name has no value, the database rejects the query, a column name appears twice, or reading all rows takes longer than timeout.",
		Limits:  []string{"timeout limits the query with reading all rows (default 5m); the rows are read as a stream."},
	},
}

type tableInput struct {
	Connection string            `json:"connection" doc:"Connection of the database (a file in connections/)" schema:"required,connection=sqlite|sqlserver|postgres,example=app-db"`
	SQL        string            `json:"sql" doc:"The query; values as @name with the value under params, never written into the query" schema:"required,format=runnel-sql,example=SELECT * FROM customers"`
	Params     map[string]string `json:"params" doc:"Values of the parameters @name of the query; may use ${…}"`
	Timeout    string            `json:"timeout" doc:"Longest time the query may take, reading all rows included" schema:"format=runnel-duration,default=5m"`
}

func newTableInput(with map[string]any) (transform.Transform, error) {
	t := &tableInput{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	if _, err := sqldb.Timeout(t.Timeout); err != nil {
		return nil, err
	}
	if _, err := sqldb.Args(t.SQL, t.Params); err != nil {
		return nil, err
	}
	return t, nil
}

// Init reports an open schema: the columns are only known when the query runs, and
// checking a pipeline does not open the database.
func (t *tableInput) Init(*transform.InitContext) (record.Schema, error) {
	return record.OpenSchema(), nil
}

func (t *tableInput) Process(ctx context.Context, _ <-chan transform.Batch, out *transform.Output) error {
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
	stmt, args, err := sqldb.Statement(conn, t.SQL, t.Params)
	if err != nil {
		return err
	}
	timeout, _ := sqldb.Timeout(t.Timeout)
	qctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err = t.read(qctx, db, stmt, args, out)
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return ctx.Err() // the pipeline was stopped
	case errors.Is(qctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("the query took longer than %s", timeout)
	}
	return err
}

func (t *tableInput) read(ctx context.Context, db *sql.DB, stmt string, args []any, out *transform.Output) error {
	rows, err := db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return fmt.Errorf("connection %s: %w", t.Connection, err)
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.ColumnTypes()
	if err != nil {
		return err
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name()
		if slices.Contains(names[:i], names[i]) {
			return fmt.Errorf("the query returns the column %q twice; give one of them another name with AS", names[i])
		}
	}
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		fields := make([]record.Field, len(cols))
		for i, c := range cols {
			fields[i] = record.Field{Name: names[i], Value: sqldb.Value(values[i], c.DatabaseTypeName())}
		}
		if err := out.Emit(record.FromFields(fields)); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("connection %s: %w", t.Connection, err)
	}
	return nil
}
