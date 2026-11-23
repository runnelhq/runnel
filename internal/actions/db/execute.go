package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/sqldb"
)

// db.execute runs a statement that changes data or tables (INSERT, UPDATE, DELETE,
// CREATE …) on the database of a connection.
var executeDefinition = action.Definition{
	Type:        "db.execute",
	Category:    action.CategoryDatabase,
	Label:       "Execute SQL",
	Description: "Runs an SQL statement on a database, e.g. to insert, update or delete rows",
	Example:     "connection: app-db\nsql: DELETE FROM orders WHERE created < @before\nparams:\n  before: ${CUTOFF_DATE}\n",
	Config:      executeAction{},
	New:         newExecute,
}

type executeAction struct {
	Connection string            `json:"connection" doc:"Connection of the database (a file in connections/)" schema:"required,connection=sqlite,example=app-db"`
	SQL        string            `json:"sql" doc:"The statement; values as @name with the value under params, never written into the statement" schema:"required,format=runnel-sql,example=DELETE FROM orders WHERE status = 'cancelled'"`
	Params     map[string]string `json:"params" doc:"Values of the parameters @name of the statement; may use ${…} and {{ … }}"`
	Timeout    string            `json:"timeout" doc:"Longest time the statement may take" schema:"format=runnel-duration,default=5m"`
}

func newExecute(with map[string]any) (action.Action, error) {
	a := &executeAction{}
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

func (a *executeAction) timeout() (time.Duration, error) {
	if a.Timeout == "" {
		return sqldb.DefaultTimeout, nil
	}
	d, err := time.ParseDuration(a.Timeout)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid timeout %q (examples: 30s, 5m)", a.Timeout)
	}
	return d, nil
}

func (a *executeAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
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
	args, err := sqldb.Args(a.SQL, a.Params)
	if err != nil {
		return action.Result{}, err
	}
	timeout, _ := a.timeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := db.ExecContext(ctx, a.SQL, args...)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return action.Result{}, fmt.Errorf("the statement took longer than %s", timeout)
		}
		return action.Result{}, fmt.Errorf("connection %s: %w", a.Connection, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		n = -1
	}
	ec.Log(slog.LevelInfo, fmt.Sprintf("%d %s affected", n, plural(n)))
	return action.Result{Output: map[string]any{"rowsAffected": n}}, nil
}

func plural(n int64) string {
	if n == 1 {
		return "row"
	}
	return "rows"
}
