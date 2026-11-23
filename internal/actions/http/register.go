// Package http contains the workflow actions of the namespace "http": http.response
// (S38b1) and http.request (S10).
//
// Each action lives in a file of its own (<name>.go) with its tests next to it
// (<name>_test.go); Register lists all actions of the namespace (ADR 0013).
package http

import "github.com/runnelhq/runnel/internal/action"

// Register adds all actions of the namespace "http" to a registry.
func Register(r *action.Registry) {
	r.Register(requestDefinition)
	r.Register(responseDefinition)
}
