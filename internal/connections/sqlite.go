package connections

import "github.com/runnelhq/runnel/internal/connection"

// TypeSQLite is the connection type of an SQLite database file.
const TypeSQLite = "sqlite"

var sqliteDefinition = connection.Definition{
	Type:        TypeSQLite,
	Label:       "SQLite",
	Description: "A database in a single file, e.g. for local data and tests",
	Config:      SQLite{},
	Example:     example("shop", "Orders of the shop", TypeSQLite, "  path: data/shop.db\n"),
	Errors: "Fails when the folder of the database file cannot be created, the file is not an SQLite database or, " +
		"with readOnly: true, does not exist. A locked database is waited for up to 5 seconds.",
	Test: "Opens the database file and reads the version of SQLite. A file that does not exist yet is reported, " +
		"not created; a run creates it.",
}

// SQLite are the fields of a connection of type sqlite.
type SQLite struct {
	Path     string `json:"path" doc:"Database file, relative to the project folder; created with its folder if it does not exist" schema:"required,example=data/app.db,group=Database"`
	ReadOnly bool   `json:"readOnly" doc:"Open the database for reading only" schema:"default=false,group=Database"`
}
