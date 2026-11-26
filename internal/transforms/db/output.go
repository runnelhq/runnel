package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/sqldb"
	"github.com/runnelhq/runnel/internal/transform"
)

// db.table-output inserts each record as a new row; db.upsert inserts it or, if a row
// with the same keys exists, updates that row. Both write in transactions of commitSize
// rows and pass the written records on.
var tableOutputDefinition = transform.Definition{
	Type:         "db.table-output",
	Category:     transform.CategoryOutput,
	Label:        "Table Output",
	Description:  "Inserts each record as a new row into a database table",
	Example:      "connection: app-db\ntable: orders\nfields: [id, customer, amount]\ncommitSize: 1000\n",
	Config:       tableOutput{},
	New:          newTableOutput,
	RecordErrors: true,
	Reference: action.Reference{
		Records: "Passes every written record on unchanged to all data links, once its transaction is committed.",
		Errors:  "When a transaction fails, its rows are written again one by one with an error link, so that only the failing records go there; without one, the pipeline stops and the transactions committed before stay.",
		Limits:  []string{"commitSize rows per transaction (default 1000); timeout limits each transaction (default 5m)."},
	},
}

var upsertDefinition = transform.Definition{
	Type:         "db.upsert",
	Category:     transform.CategoryOutput,
	Label:        "Insert or Update",
	Description:  "Updates the row with the same keys in a database table, or inserts the record as a new row",
	Example:      "connection: app-db\ntable: customers\nkeys: [id]\nfields: [id, name, email]\n",
	Config:       upsert{},
	New:          newUpsert,
	RecordErrors: true,
	Reference: action.Reference{
		Records: "Passes every written record on unchanged to all data links, once its transaction is committed.",
		Errors:  "When a transaction fails, its rows are written again one by one with an error link, so that only the failing records go there; without one, the pipeline stops and the transactions committed before stay.",
		Limits:  []string{"commitSize rows per transaction (default 1000); timeout limits each transaction (default 5m)."},
	},
}

type tableOutput struct {
	Connection string   `json:"connection" doc:"Connection of the database (a file in connections/)" schema:"required,connection=sqlite|sqlserver|postgres,example=app-db"`
	Table      string   `json:"table" doc:"Table to write to, e.g. orders or sales.orders" schema:"required,example=orders"`
	Fields     []string `json:"fields" doc:"Fields to write, each into the column of the same name; empty: all fields (not those starting with _)" schema:"format=runnel-field"`
	CommitSize int      `json:"commitSize" doc:"Rows per transaction" schema:"default=1000"`
	Timeout    string   `json:"timeout" doc:"Longest time one transaction may take" schema:"format=runnel-duration,default=5m"`
}

type upsert struct {
	tableOutput
	Keys   []string `json:"keys" doc:"Fields that identify a row; SQLite and PostgreSQL need a unique index on them" schema:"required,format=runnel-field"`
	Update []string `json:"update" doc:"Fields to change in an existing row; empty: all written fields except the keys" schema:"format=runnel-field"`
}

func newTableOutput(with map[string]any) (transform.Transform, error) {
	t := &tableOutput{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	if err := t.check(); err != nil {
		return nil, err
	}
	return &writer{cfg: t}, nil
}

func newUpsert(with map[string]any) (transform.Transform, error) {
	u := &upsert{}
	if err := transform.DecodeConfig(with, u); err != nil {
		return nil, err
	}
	if err := u.check(); err != nil {
		return nil, err
	}
	if len(u.Keys) == 0 {
		return nil, errors.New("keys: name at least one field that identifies a row")
	}
	for _, list := range [][]string{u.Keys, u.Update} {
		for _, f := range list {
			if err := sqldb.CheckColumn(f); err != nil {
				return nil, err
			}
			if len(u.Fields) > 0 && !slices.Contains(u.Fields, f) {
				return nil, fmt.Errorf("%s is not one of the fields to write (fields)", f)
			}
		}
	}
	return &writer{cfg: &u.tableOutput, keys: u.Keys, update: u.Update}, nil
}

func (t *tableOutput) check() error {
	if err := sqldb.CheckTable(t.Table); err != nil {
		return err
	}
	for _, f := range t.Fields {
		if err := sqldb.CheckColumn(f); err != nil {
			return err
		}
	}
	if t.CommitSize < 0 {
		return errors.New("commitSize must not be negative")
	}
	_, err := sqldb.Timeout(t.Timeout)
	return err
}

// writer writes records to a table: inserts only, or upserts with keys.
type writer struct {
	cfg    *tableOutput
	keys   []string // upsert: the keys; nil for db.table-output
	update []string
}

// Init passes the incoming schema on and checks that the named fields come in.
func (w *writer) Init(ic *transform.InitContext) (record.Schema, error) {
	for name, list := range map[string][]string{"fields": w.cfg.Fields, "keys": w.keys, "update": w.update} {
		for i, f := range list {
			ic.Use(f, fmt.Sprintf("%s[%d]", name, i))
		}
	}
	return ic.In, nil
}

func (w *writer) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	conn, err := out.Connection(ctx, w.cfg.Connection)
	if err != nil {
		return err
	}
	db, err := sqldb.Open(ctx, connections.Builtin(), conn)
	if err != nil {
		return err
	}
	d := sqldb.DialectOf(conn.Type)
	size := w.cfg.CommitSize
	if size == 0 {
		size = 1000
	}
	timeout, _ := sqldb.Timeout(w.cfg.Timeout)
	var written, failed int
	buf := make([]record.Record, 0, size)
	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		defer func() { buf = buf[:0] }()
		err := w.write(ctx, db, d, timeout, buf)
		switch {
		case err == nil:
		case ctx.Err() != nil:
			return ctx.Err()
		case !out.HasErrorLinks():
			return err
		default:
			// The transaction was rolled back: once more row by row, so that only the rows
			// that fail go to the error link.
			for _, r := range buf {
				if err := w.write(ctx, db, d, timeout, []record.Record{r}); err != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					failed++
					if err := out.Fail(r, "", err); err != nil {
						return err
					}
					continue
				}
				written++
				if err := out.Emit(r); err != nil {
					return err
				}
			}
			return nil
		}
		for _, r := range buf {
			if err := out.Emit(r); err != nil {
				return err
			}
		}
		written += len(buf)
		return nil
	}
	for b := range in {
		for _, r := range b {
			buf = append(buf, r)
			if len(buf) == size {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	msg := fmt.Sprintf("wrote %d %s to %s", written, rows(written), w.cfg.Table)
	if failed > 0 {
		msg += fmt.Sprintf(", %d failed", failed)
	}
	out.Logf(slog.LevelInfo, "%s", msg)
	return nil
}

func rows(n int) string {
	if n == 1 {
		return "row"
	}
	return "rows"
}

// write writes records in one transaction.
func (w *writer) write(ctx context.Context, db *sql.DB, d sqldb.Dialect, timeout time.Duration, recs []record.Record) (err error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return w.fail(ctx, timeout, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	stmts := map[string]*sql.Stmt{} // by statement text: records with the same fields share one
	prepare := func(text string) (*sql.Stmt, error) {
		if s := stmts[text]; s != nil {
			return s, nil
		}
		s, err := tx.PrepareContext(ctx, text)
		stmts[text] = s
		return s, err
	}
	for _, r := range recs {
		if err := w.writeOne(ctx, d, prepare, r); err != nil {
			return w.fail(ctx, timeout, err)
		}
	}
	return w.fail(ctx, timeout, tx.Commit())
}

// writeOne writes one record with the prepared statements of the transaction.
func (w *writer) writeOne(ctx context.Context, d sqldb.Dialect, prepare func(string) (*sql.Stmt, error), r record.Record) error {
	cols := w.cfg.Fields
	if len(cols) == 0 {
		for _, n := range r.Names() {
			if !strings.HasPrefix(n, "_") {
				cols = append(cols, n)
			}
		}
		for _, c := range cols {
			if err := sqldb.CheckColumn(c); err != nil {
				return err
			}
		}
	}
	values := make(map[string]any, len(cols))
	for _, c := range cols {
		v, _ := r.Get(c)
		a, err := sqldb.Arg(v)
		if err != nil {
			return fmt.Errorf("field %s: %w", c, err)
		}
		values[c] = a
	}
	args := func(names []string) []any {
		out := make([]any, len(names))
		for i, n := range names {
			out[i] = values[n]
		}
		return out
	}
	if w.keys == nil {
		return execOn(ctx, prepare, d.Insert(w.cfg.Table, cols), args(cols))
	}
	update := w.update
	if len(update) == 0 {
		for _, c := range cols {
			if !slices.Contains(w.keys, c) {
				update = append(update, c)
			}
		}
	}
	if !d.SplitUpsert() {
		return execOn(ctx, prepare, d.Upsert(w.cfg.Table, cols, w.keys, update), args(cols))
	}
	// SQL Server: change the row; if there is none, insert it.
	set := update
	if len(set) == 0 {
		set = w.keys[:1]
	}
	s, err := prepare(d.Update(w.cfg.Table, w.keys, update))
	if err != nil {
		return err
	}
	res, err := s.ExecContext(ctx, append(args(set), args(w.keys)...)...)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n > 0 {
		return err
	}
	return execOn(ctx, prepare, d.Insert(w.cfg.Table, cols), args(cols))
}

func execOn(ctx context.Context, prepare func(string) (*sql.Stmt, error), text string, args []any) error {
	s, err := prepare(text)
	if err != nil {
		return err
	}
	_, err = s.ExecContext(ctx, args...)
	return err
}

// fail words an error of a transaction: a timeout as such, other errors with the table.
func (w *writer) fail(ctx context.Context, timeout time.Duration, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("writing to %s took longer than %s", w.cfg.Table, timeout)
	}
	return fmt.Errorf("%s: %w", w.cfg.Table, err)
}
