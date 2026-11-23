package connections

import "github.com/runnelhq/runnel/internal/connection"

// TypeSQLite is the connection type of an SQLite database file.
const TypeSQLite = "sqlite"

var sqliteDefinition = connection.Definition{
	Type:        TypeSQLite,
	Label:       "SQLite",
	Description: "A database in a single file, e.g. for local data and tests",
	Config:      SQLite{},
}

// SQLite are the fields of a connection of type sqlite.
type SQLite struct {
	Path     string `json:"path" doc:"Database file, relative to the project folder; created with its folder if it does not exist" schema:"required,example=data/app.db"`
	ReadOnly bool   `json:"readOnly" doc:"Open the database for reading only" schema:"default=false"`
}
