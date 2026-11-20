// Package core contains the basic transforms of the namespace "core": core.generate-rows,
// core.dummy and core.log (roadmap S13), core.filter (S16a), core.select-fields,
// core.constant and core.convert (S16b), core.switch (S17b), core.sort and core.group-by
// (S22), core.from-result and core.to-result (S24a). Each transform lives in a
// file of its own with its tests next to it; Register lists all of them (ADR 0013).
package core

import "github.com/runnelhq/runnel/internal/transform"

// Register adds all transforms of the namespace "core" to a registry.
func Register(r *transform.Registry) {
	r.Register(generateRowsDefinition)
	r.Register(dummyDefinition)
	r.Register(filterDefinition)
	r.Register(selectFieldsDefinition)
	r.Register(constantDefinition)
	r.Register(convertDefinition)
	r.Register(switchDefinition)
	r.Register(sortDefinition)
	r.Register(groupByDefinition)
	r.Register(fromResultDefinition)
	r.Register(toResultDefinition)
	r.Register(logDefinition)
}
