package archive

import "github.com/runnelhq/runnel/internal/action"

// Register adds the all actions of the namespace "archive" to the registry.
func Register(r *action.Registry) {
	r.Register(createDefinition)
	r.Register(extractDefinition)
}
