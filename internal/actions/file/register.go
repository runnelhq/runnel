package file

import "github.com/runnelhq/runnel/internal/action"

// Register adds all actions of the namespace "file" to a registry.
func Register(r *action.Registry) {
	r.Register(existsDefinition)
	r.Register(waitDefinition)
	r.Register(copyDefinition)
	r.Register(moveDefinition)
	r.Register(deleteDefinition)
	r.Register(mkdirDefinition)
	r.Register(listDefinition)
}
