// Package actions assembles all actions built into Runnel.
//
// Each namespace is a package below internal/actions (e.g. internal/actions/core)
// with a Register function; this package is the single place that lists the
// namespaces. Everything that needs the built-in actions – CLI, server, MCP –
// uses Builtin (ADR 0013).
package actions

import (
	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions/archive"
	"github.com/runnelhq/runnel/internal/actions/core"
	"github.com/runnelhq/runnel/internal/actions/file"
	"github.com/runnelhq/runnel/internal/actions/http"
)

// namespaces lists the Register functions of all built-in namespaces.
// A new namespace is added here and nowhere else.
var namespaces = []func(*action.Registry){
	archive.Register,
	core.Register,
	file.Register,
	http.Register,
}

// Builtin returns a new registry with all built-in actions.
func Builtin() *action.Registry {
	r := action.NewRegistry()
	for _, register := range namespaces {
		register(r)
	}
	return r
}
