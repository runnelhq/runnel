// Package json contains the transforms of the namespace "json" (roadmap S19): json.input
// reads a JSON or NDJSON file, json.output writes one. Each transform lives in a file of
// its own; this file and values.go hold what they share.
package json

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"

	"github.com/runnelhq/runnel/internal/transform"
)

// Register adds all transforms of the namespace "json" to a registry.
func Register(r *transform.Registry) {
	r.Register(inputDefinition)
	r.Register(outputDefinition)
}

// Formats of a file: one JSON document (an array of records, or one object) or NDJSON
// (one JSON value per line, also called JSON Lines).
const (
	formatAuto   = "auto"
	formatJSON   = "json"
	formatNDJSON = "ndjson"
)

const levelInfo = slog.LevelInfo

func plural(n int64, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// plain returns the reason of a file system error without the operation and path,
// which the messages name themselves.
func plain(err error) error {
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return pe.Err
	}
	if le, ok := errors.AsType[*os.LinkError](err); ok {
		return le.Err
	}
	return err
}
