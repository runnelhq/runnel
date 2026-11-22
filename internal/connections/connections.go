// Package connections assembles the connection types built into Runnel (ADR 0022):
// one file per type with its configuration struct. Everything that checks or opens
// connections – CLI, server, runs – uses Builtin.
package connections

import "github.com/runnelhq/runnel/internal/connection"

// Builtin returns a new registry with all built-in connection types.
func Builtin() *connection.Registry {
	r := connection.NewRegistry()
	r.Register(sqliteDefinition)
	return r
}
