// Package db contains the transforms of the namespace "db" (ADR 0022): reading and
// writing the databases of connections, with values only as parameters @name.
//
// Each transform lives in a file of its own (<name>.go) with its tests next to it
// (<name>_test.go); Register lists all transforms of the namespace (ADR 0013).
package db

import "github.com/runnelhq/runnel/internal/transform"

// Register adds all transforms of the namespace "db" to a registry.
func Register(r *transform.Registry) {
	r.Register(tableInputDefinition)
	r.Register(tableOutputDefinition)
	r.Register(upsertDefinition)
}
