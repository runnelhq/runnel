package conntest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/sqldb"
)

func TestSQLite(t *testing.T) {
	dir := t.TempDir()
	ctx := paths.WithBase(context.Background(), dir)
	c := connection.Resolved{Name: "db", Type: connections.TypeSQLite, With: map[string]any{"path": "data/app.db"}}
	msg, err := Run(ctx, c)
	if err != nil || msg != "the database file data/app.db does not exist yet; a run creates it" {
		t.Fatalf("%q %v", msg, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "data")); !os.IsNotExist(err) {
		t.Error("the test created the folder")
	}
	// An existing database answers with its version.
	db, err := sqldb.Open(ctx, connections.Builtin(), c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE t (n INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if msg, err := Run(ctx, c); err != nil || !strings.HasPrefix(msg, "SQLite 3.") {
		t.Errorf("%q %v", msg, err)
	}
}

func TestUnknownType(t *testing.T) {
	if _, err := Run(context.Background(), connection.Resolved{Name: "x", Type: "ftp"}); err == nil || !strings.Contains(err.Error(), "cannot be tested yet") {
		t.Errorf("%v", err)
	}
}

// sftp is tested by transfer.Check (tests with a server there); here only that Run uses it.
func TestSFTP(t *testing.T) {
	c := connection.Resolved{Name: "files", Type: connections.TypeSFTP, With: map[string]any{"host": "127.0.0.1", "port": "1", "user": "runnel", "password": "pw", "timeout": "2s"}}
	if _, err := Run(context.Background(), c); err == nil || !strings.Contains(err.Error(), "connection files: cannot connect to 127.0.0.1:1") {
		t.Errorf("%v", err)
	}
}
