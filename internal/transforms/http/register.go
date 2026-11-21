// Package http contains the transforms of the namespace "http": http.response (S38c).
// Each transform lives in a file of its own with its tests next to it; Register lists
// all of them (ADR 0013).
package http

import "github.com/runnelhq/runnel/internal/transform"

// Register adds all transforms of the namespace "http" to a registry.
func Register(r *transform.Registry) {
	r.Register(responseDefinition)
}
