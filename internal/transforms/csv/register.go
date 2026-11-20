package csv

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"

	"github.com/runnelhq/runnel/internal/transform"
)

// Register adds all transforms of the namespace "csv" to a registry.
func Register(r *transform.Registry) {
	r.Register(inputDefinition)
	r.Register(outputDefinition)
}

const (
	levelInfo = slog.LevelInfo
	levelWarn = slog.LevelWarn
)

func plural(n int64, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// plain returns the reason of a file system error without the operation and path,
// which the messages name themselves.
func plain(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// plainLink is plain for errors of renaming a file.
func plainLink(err error) error {
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err
	}
	return plain(err)
}
