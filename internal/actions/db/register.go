// Package db contains the actions of the namespace "db" (ADR 0022): statements on the
// databases of connections, with values only as parameters @name.
//
// Each action lives in a file of its own (<name>.go) with its tests next to it
// (<name>_test.go); Register lists all actions of the namespace (ADR 0013).
package db

import "github.com/runnelhq/runnel/internal/action"

// Register adds all actions of the namespace "db" to a registry.
func Register(r *action.Registry) {
	r.Register(executeDefinition)
	r.Register(queryDefinition)
}
