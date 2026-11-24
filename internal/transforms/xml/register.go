// Package xml contains the transforms of the namespace "xml" (roadmap S68): xml.input
// reads the elements of an XML file as records, xml.output writes records as elements.
// Each transform lives in a file of its own; read.go holds the reading of the elements.
package xml

import (
	"errors"
	"io/fs"
	"log/slog"

	"github.com/runnelhq/runnel/internal/transform"
)

// Register adds all transforms of the namespace "xml" to a registry.
func Register(r *transform.Registry) {
	r.Register(inputDefinition)
	r.Register(outputDefinition)
}

const levelInfo = slog.LevelInfo

func plural(n int64, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// plain returns the reason of a file system error without the operation and path.
func plain(err error) error {
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return pe.Err
	}
	return err
}
