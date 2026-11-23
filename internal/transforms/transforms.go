// Package transforms assembles all transforms built into Runnel, like package actions
// for actions (ADR 0013): each namespace is a package below internal/transforms with a
// Register function, listed here and nowhere else.
package transforms

import (
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/core"
	"github.com/runnelhq/runnel/internal/transforms/csv"
	"github.com/runnelhq/runnel/internal/transforms/db"
	"github.com/runnelhq/runnel/internal/transforms/file"
	"github.com/runnelhq/runnel/internal/transforms/http"
	"github.com/runnelhq/runnel/internal/transforms/json"
)

var namespaces = []func(*transform.Registry){
	core.Register,
	csv.Register,
	db.Register,
	file.Register,
	json.Register,
	http.Register,
}

// Builtin returns a new registry with all built-in transforms.
func Builtin() *transform.Registry {
	r := transform.NewRegistry()
	for _, register := range namespaces {
		register(r)
	}
	return r
}
