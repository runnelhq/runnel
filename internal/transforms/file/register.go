// Package file contains the transforms of the namespace "file": file.list reads the
// entries of a folder as records. Each transform lives in a file of its own (ADR 0013).
package file

import "github.com/runnelhq/runnel/internal/transform"

// Register adds all transforms of the namespace "file" to a registry.
func Register(r *transform.Registry) {
	r.Register(listDefinition)
}
