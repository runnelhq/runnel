package excel

import "github.com/runnelhq/runnel/internal/transform"

// Register adds all transforms of the namespace "excel" to a registry.
func Register(r *transform.Registry) {
	r.Register(inputDefinition)
}
