// Package core contains the basic transforms of the namespace "core" (roadmap S13):
// core.generate-rows, core.dummy and core.log. Each transform lives in a file of its
// own with its tests next to it; Register lists all of them (ADR 0013).
package core

import "github.com/runnelhq/runnel/internal/transform"

// Register adds all transforms of the namespace "core" to a registry.
func Register(r *transform.Registry) {
	r.Register(generateRowsDefinition)
	r.Register(dummyDefinition)
	r.Register(logDefinition)
}
