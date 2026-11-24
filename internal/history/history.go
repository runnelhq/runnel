// Package history keeps the runs of a project in a SQLite file in the data folder
// (ADR 0026): the run, the state of its actions or transforms and its log. Server,
// service and runnel run may open the same file at the same time (WAL). The package
// stores the API's JSON of a run as it is; the columns are what lists filter on.
package history

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite" // the driver "sqlite" (pure Go, BSD-3), as for connections (ADR 0022)
)

// Limits of what a run stores (ADR 0026).
const (
	// MaxLogLines is the number of log lines kept per run; older ones are dropped.
	MaxLogLines = 5000
	// MaxLineBytes is the longest log line kept; longer ones are cut.
	MaxLineBytes = 8 << 10
	// MaxDataBytes is the largest JSON of a run (with outputs and parameters).
	MaxDataBytes = 1 << 20
	// BeatEvery is how often a running run writes a sign of life; a run without one for
	// StaleAfter is taken as ended by a stopped process.
	BeatEvery  = 30 * time.Second
	StaleAfter = 2 * time.Minute
	// StaleError is the error of such a run.
	StaleError = "runnel stopped while the run was running"
)

// FileName is the name of the history file in the folder of a project.
const FileName = "runs.db"

// ErrNewer is returned by Open for a file written by a newer runnel; it is not changed.
var ErrNewer = errors.New("the history was written by a newer runnel")

// Run is a run as stored: the columns lists filter on, and the JSON of the run as the
// API shows it.
type Run struct {
	ID       string
	Path     string
	Kind     string
	Trigger  string
	Status   string
	Started  time.Time
	Finished *time.Time
	Error    string
	// FileHash identifies the content of the workflow or pipeline that ran.
	FileHash string
	Data     []byte
}

// Node is the state of an action or transform of a run, as JSON.
type Node struct {
	Index int
	ID    string
	Data  []byte
}

// LogLine is a line of the log of a run.
type LogLine struct {
	Seq      int64
	Time     time.Time
	Level    string
	ActionID string
	Message  string
}

// Store is the history of one project.
type Store struct {
	db   *sql.DB
	path string
}

// DefaultBase returns the folder that holds the histories of the user's projects:
// %LOCALAPPDATA%\runnel\history on Windows, $XDG_STATE_HOME/runnel/history (else
// ~/.local/state/runnel/history) elsewhere.
func DefaultBase() (string, error) {
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			var err error
			if base, err = os.UserCacheDir(); err != nil {
				return "", err
			}
		}
		return filepath.Join(base, "runnel", "history"), nil
	}
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, "runnel", "history"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "runnel", "history"), nil
}

// ProjectDir returns the folder of a project's history in base: the name of the
// project folder and the first 8 characters of SHA-256 over its absolute path.
func ProjectDir(base, projectDir string) (string, error) {
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return "", err
	}
	key := filepath.Clean(abs)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key) // C:\Data and c:\data are the same folder
	}
	sum := sha256.Sum256([]byte(key))
	name := filepath.Base(abs)
	if name == "" || name == "." || name == string(filepath.Separator) || strings.HasSuffix(name, ":\\") {
		name = "project"
	}
	return filepath.Join(base, name+"-"+hex.EncodeToString(sum[:4])), nil
}

// Open opens the history in dir (created if needed), brings its tables up to date and
// marks runs of stopped processes as failed.
func Open(ctx context.Context, dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("cannot create the history folder %s: %w", dir, err)
	}
	path := filepath.Join(dir, FileName)
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("cannot open the history %s: %w", path, err)
	}
	s := &Store{db: db, path: path}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		if errors.Is(err, ErrNewer) {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return nil, fmt.Errorf("cannot open the history %s: %w", path, err)
	}
	if err := s.MarkStale(ctx, time.Now()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cannot open the history %s: %w", path, err)
	}
	return s, nil
}

// Path returns the file of the history.
func (s *Store) Path() string { return s.path }

// Close closes the history.
func (s *Store) Close() error { return s.db.Close() }

// migrations bring the tables from one version (index) to the next. Only forward; the
// version is in PRAGMA user_version.
var migrations = []string{
	`PRAGMA auto_vacuum = INCREMENTAL;
	VACUUM;
	CREATE TABLE runs (
		id        TEXT PRIMARY KEY,
		path      TEXT NOT NULL,
		kind      TEXT NOT NULL,
		trigger   TEXT NOT NULL DEFAULT '',
		status    TEXT NOT NULL,
		started   INTEGER NOT NULL,
		finished  INTEGER,
		heartbeat INTEGER NOT NULL,
		error     TEXT NOT NULL DEFAULT '',
		file_hash TEXT NOT NULL DEFAULT '',
		parent_id TEXT NOT NULL DEFAULT '',
		data      TEXT NOT NULL
	);
	CREATE INDEX runs_status ON runs (status);
	CREATE INDEX runs_path ON runs (path, id);
	CREATE TABLE nodes (
		run_id TEXT NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
		idx    INTEGER NOT NULL,
		id     TEXT NOT NULL,
		data   TEXT NOT NULL,
		PRIMARY KEY (run_id, idx)
	);
	CREATE TABLE log (
		run_id    TEXT NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
		seq       INTEGER NOT NULL,
		time      INTEGER NOT NULL,
		level     TEXT NOT NULL,
		action_id TEXT NOT NULL DEFAULT '',
		message   TEXT NOT NULL,
		PRIMARY KEY (run_id, seq)
	);`,
}

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return ErrNewer
	}
	for v := version; v < len(migrations); v++ {
		// auto_vacuum and VACUUM cannot run in a transaction; the first migration only
		// runs on a new, empty file.
		if _, err := s.db.ExecContext(ctx, migrations[v]+fmt.Sprintf("\nPRAGMA user_version = %d;", v+1)); err != nil {
			// Another process (e.g. the service and runnel run) may have migrated it.
			var now int
			if s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&now) == nil && now > v {
				continue
			}
			return err
		}
	}
	return nil
}

func ms(t time.Time) int64 { return t.UnixMilli() }

func fromMs(v int64) time.Time { return time.UnixMilli(v) }

// execer runs statements on the database or in a transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
}

// Tx changes the history in one transaction (Store.Update).
type Tx struct{ ex execer }

// Update runs fn in one transaction: all its changes are written, or none. Writing many
// changes in one transaction is much faster than one by one.
func (s *Store) Update(ctx context.Context, fn func(*Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(&Tx{ex: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

// PutRun inserts or replaces a run; its sign of life is set to now.
func (s *Store) PutRun(ctx context.Context, r Run) error {
	return s.Update(ctx, func(tx *Tx) error { return tx.PutRun(ctx, r) })
}

// PutNode inserts or replaces the state of an action or transform of a run.
func (s *Store) PutNode(ctx context.Context, runID string, n Node) error {
	return s.Update(ctx, func(tx *Tx) error { return tx.PutNode(ctx, runID, n) })
}

// AddLog appends log lines of a run (see Tx.AddLog).
func (s *Store) AddLog(ctx context.Context, runID string, lines []LogLine) error {
	return s.Update(ctx, func(tx *Tx) error { return tx.AddLog(ctx, runID, lines) })
}

// Delete removes a run with its actions and log.
func (s *Store) Delete(ctx context.Context, id string) error {
	return s.Update(ctx, func(tx *Tx) error { return tx.Delete(ctx, id) })
}

// PutRun inserts or replaces a run; its sign of life is set to now.
func (t *Tx) PutRun(ctx context.Context, r Run) error {
	if len(r.Data) > MaxDataBytes {
		return fmt.Errorf("run %s: its data has %d bytes, more than %d", r.ID, len(r.Data), MaxDataBytes)
	}
	var finished any
	if r.Finished != nil {
		finished = ms(*r.Finished)
	}
	_, err := t.ex.ExecContext(ctx, `INSERT INTO runs (id, path, kind, trigger, status, started, finished, heartbeat, error, file_hash, data)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET path = excluded.path, kind = excluded.kind, trigger = excluded.trigger,
			status = excluded.status, started = excluded.started, finished = excluded.finished,
			heartbeat = excluded.heartbeat, error = excluded.error, file_hash = excluded.file_hash, data = excluded.data`,
		r.ID, r.Path, r.Kind, r.Trigger, r.Status, ms(r.Started), finished, ms(time.Now()), r.Error, r.FileHash, string(r.Data))
	return err
}

// PutNode inserts or replaces the state of an action or transform of a run.
func (t *Tx) PutNode(ctx context.Context, runID string, n Node) error {
	_, err := t.ex.ExecContext(ctx, `INSERT INTO nodes (run_id, idx, id, data) VALUES (?, ?, ?, ?)
		ON CONFLICT (run_id, idx) DO UPDATE SET id = excluded.id, data = excluded.data`, runID, n.Index, n.ID, string(n.Data))
	return err
}

// AddLog appends log lines of a run and drops the oldest beyond MaxLogLines. Lines
// longer than MaxLineBytes are cut.
func (t *Tx) AddLog(ctx context.Context, runID string, lines []LogLine) error {
	if len(lines) == 0 {
		return nil
	}
	stmt, err := t.ex.PrepareContext(ctx, `INSERT OR REPLACE INTO log (run_id, seq, time, level, action_id, message) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	var last int64
	for _, l := range lines {
		if _, err := stmt.ExecContext(ctx, runID, l.Seq, ms(l.Time), l.Level, l.ActionID, cut(l.Message, MaxLineBytes)); err != nil {
			return err
		}
		last = max(last, l.Seq)
	}
	if last > MaxLogLines {
		if _, err := t.ex.ExecContext(ctx, `DELETE FROM log WHERE run_id = ? AND seq <= ?`, runID, last-MaxLogLines); err != nil {
			return err
		}
	}
	return nil
}

// cut shortens s to at most n bytes, at a character boundary, with a note.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	const note = " … (cut)"
	i := n - len(note)
	for i > 0 && i < len(s) && s[i]&0xC0 == 0x80 { // inside a UTF-8 character
		i--
	}
	return s[:i] + note
}

// Beat sets the sign of life of running runs to now.
func (s *Store) Beat(ctx context.Context, ids []string) error {
	return s.Update(ctx, func(tx *Tx) error { return tx.Beat(ctx, ids) })
}

// Beat sets the sign of life of running runs to now.
func (t *Tx) Beat(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if _, err := t.ex.ExecContext(ctx, `UPDATE runs SET heartbeat = ? WHERE id = ? AND status = 'running'`, ms(time.Now()), id); err != nil {
			return err
		}
	}
	return nil
}

// MarkStale sets runs without a sign of life for StaleAfter to failed (StaleError); they
// ended with the process that ran them. Runs of other processes that still run keep
// writing their sign of life and are not touched.
func (s *Store) MarkStale(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE runs SET status = 'failed', error = ?, finished = heartbeat
		WHERE status = 'running' AND heartbeat < ?`, StaleError, ms(now.Add(-StaleAfter)))
	return err
}

// Delete removes a run with its actions and log.
func (t *Tx) Delete(ctx context.Context, id string) error {
	_, err := t.ex.ExecContext(ctx, `DELETE FROM runs WHERE id = ?`, id)
	return err
}

const runColumns = `id, path, kind, trigger, status, started, finished, error, file_hash, data`

func scanRun(row interface{ Scan(...any) error }) (Run, error) {
	var r Run
	var started int64
	var finished sql.NullInt64
	var data string
	if err := row.Scan(&r.ID, &r.Path, &r.Kind, &r.Trigger, &r.Status, &started, &finished, &r.Error, &r.FileHash, &data); err != nil {
		return r, err
	}
	r.Started, r.Data = fromMs(started), []byte(data)
	if finished.Valid {
		t := fromMs(finished.Int64)
		r.Finished = &t
	}
	return r, nil
}

// TriggerEditor in a Filter selects the runs without trigger: those started in the editor
// or with POST /runs (S26e1). Triggers are "type:name" or "cli", so no trigger has this name.
const TriggerEditor = "editor"

// Filter selects runs for List; empty fields select all.
type Filter struct {
	Status  string // e.g. failed
	Path    string // the file of the workflow or pipeline, as in the project ("workflows/a.workflow.yaml")
	Trigger string // e.g. cron:nightly, http:orders, cli; TriggerEditor: runs without trigger
	// Before lists the runs started before the one with this id (pages: the id of the
	// last run of the page before).
	Before string
	Limit  int
}

// List returns the newest runs first, at most limit.
func (s *Store) List(ctx context.Context, limit int) ([]Run, error) {
	return s.Find(ctx, Filter{Limit: limit})
}

// Find returns the runs that match f, newest first, at most f.Limit (100 if 0).
func (s *Store) Find(ctx context.Context, f Filter) ([]Run, error) {
	if err := s.MarkStale(ctx, time.Now()); err != nil {
		return nil, err
	}
	var where []string
	var args []any
	for _, c := range []struct{ col, op, val string }{
		{"status", "=", f.Status}, {"path", "=", f.Path}, {"trigger", "=", f.Trigger}, {"id", "<", f.Before},
	} {
		if c.val != "" {
			where = append(where, c.col+" "+c.op+" ?")
			if c.col == "trigger" && c.val == TriggerEditor {
				c.val = ""
			}
			args = append(args, c.val)
		}
	}
	q := `SELECT ` + runColumns + ` FROM runs`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	if f.Limit <= 0 {
		f.Limit = 100
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY id DESC LIMIT ?`, append(args, f.Limit)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Detail is a stored run with its actions or transforms and its log.
type Detail struct {
	Run   Run
	Nodes []Node
	Log   []LogLine
	// LogTruncated is set when older log lines were dropped (MaxLogLines).
	LogTruncated bool
}

// Get returns a run with its actions and log; false if there is no such run.
func (s *Store) Get(ctx context.Context, id string) (Detail, bool, error) {
	if err := s.MarkStale(ctx, time.Now()); err != nil {
		return Detail{}, false, err
	}
	r, err := scanRun(s.db.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Detail{}, false, nil
	}
	if err != nil {
		return Detail{}, false, err
	}
	d := Detail{Run: r}
	nodes, err := s.db.QueryContext(ctx, `SELECT idx, id, data FROM nodes WHERE run_id = ? ORDER BY idx`, id)
	if err != nil {
		return Detail{}, false, err
	}
	for nodes.Next() {
		var n Node
		var data string
		if err := nodes.Scan(&n.Index, &n.ID, &data); err != nil {
			_ = nodes.Close()
			return Detail{}, false, err
		}
		n.Data = []byte(data)
		d.Nodes = append(d.Nodes, n)
	}
	if err := errors.Join(nodes.Err(), nodes.Close()); err != nil {
		return Detail{}, false, err
	}
	lines, err := s.db.QueryContext(ctx, `SELECT seq, time, level, action_id, message FROM log WHERE run_id = ? ORDER BY seq`, id)
	if err != nil {
		return Detail{}, false, err
	}
	defer func() { _ = lines.Close() }()
	for lines.Next() {
		var l LogLine
		var t int64
		if err := lines.Scan(&l.Seq, &t, &l.Level, &l.ActionID, &l.Message); err != nil {
			return Detail{}, false, err
		}
		l.Time = fromMs(t)
		d.Log = append(d.Log, l)
	}
	d.LogTruncated = len(d.Log) > 0 && d.Log[0].Seq > 1
	return d, true, lines.Err()
}

// pruneBatch is the most runs removed in one transaction, so that writers do not wait long.
const pruneBatch = 200

// Prune removes ended runs older than keepDays and those beyond the newest maxRuns,
// with their actions and log (S26c), in small transactions, and frees the space. It
// returns the number of runs removed. Running runs are kept.
func (s *Store) Prune(ctx context.Context, keepDays, maxRuns int, now time.Time) (int, error) {
	cutoff := ms(now.AddDate(0, 0, -keepDays))
	removed := 0
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM runs WHERE id IN (SELECT id FROM runs WHERE status != 'running' AND started < ? LIMIT ?)`, []any{cutoff, pruneBatch}},
		{`DELETE FROM runs WHERE id IN (SELECT id FROM runs WHERE status != 'running' AND id < (SELECT id FROM runs ORDER BY id DESC LIMIT 1 OFFSET ?) LIMIT ?)`,
			[]any{maxRuns - 1, pruneBatch}},
	} {
		for {
			if err := ctx.Err(); err != nil {
				return removed, err
			}
			res, err := s.db.ExecContext(ctx, q.sql, q.args...)
			if err != nil {
				return removed, err
			}
			n, _ := res.RowsAffected()
			removed += int(n)
			if n < pruneBatch {
				break
			}
		}
	}
	if removed > 0 {
		// Give the space of the removed runs back to the file system (auto_vacuum = INCREMENTAL).
		if _, err := s.db.ExecContext(ctx, `PRAGMA incremental_vacuum`); err != nil {
			return removed, err
		}
	}
	return removed, nil
}
