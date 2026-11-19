// Package webui embeds the editor (the build of web/, see docs/dev/EDITOR.md) into the
// binary. Checkouts without a frontend build contain only a placeholder; the server then
// explains how to build the editor.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the editor files (index.html, assets/ …).
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // the directory is embedded, so this cannot happen
	}
	return sub
}
