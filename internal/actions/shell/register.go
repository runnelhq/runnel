// Package shell contains the actions of the namespace "shell" (ADR 0023): shell.run
// starts a program without a shell (S60). Each action lives in a file of its own.
package shell

import "github.com/runnelhq/runnel/internal/action"

// Register adds all actions of the namespace "shell" to a registry.
func Register(r *action.Registry) {
	r.Register(runDefinition)
}
