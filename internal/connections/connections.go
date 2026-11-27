// Package connections assembles the connection types built into Runnel (ADR 0022):
// one file per type with its configuration struct. Everything that checks or opens
// connections – CLI, server, runs – uses Builtin.
package connections

import "github.com/runnelhq/runnel/internal/connection"

// Builtin returns a new registry with all built-in connection types.
func Builtin() *connection.Registry {
	r := connection.NewRegistry()
	r.Register(sqliteDefinition)
	r.Register(sqlServerDefinition)
	r.Register(postgresDefinition)
	r.Register(httpDefinition)
	r.Register(smtpDefinition)
	r.Register(sftpDefinition)
	r.Register(ftpsDefinition)
	r.Register(ftpDefinition)
	return r
}

// example returns a connection file of a type for the catalog (Definition.Example); with
// are the fields under with:, each line indented by two spaces.
func example(name, description, typ, with string) string {
	return "kind: Connection\napiVersion: runnel/v1\nname: " + name + "\ndescription: " + description +
		"\ntype: " + typ + "\nwith:\n" + with
}
