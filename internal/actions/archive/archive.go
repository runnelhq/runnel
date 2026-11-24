// Package archive contains the actions of the namespace "archive" (roadmap S54):
// archive.create packs files and folders into an archive, archive.extract unpacks one.
// Formats: ZIP, TAR and TAR.GZ, chosen by the file name or, when unpacking, by the
// content. Each action lives in a file of its own; this file holds what both share.
package archive

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Formats of an archive.
const (
	formatAuto  = "auto"
	formatZip   = "zip"
	formatTar   = "tar"
	formatTarGz = "tar.gz"
)

func checkFormat(f string) error {
	switch f {
	case "", formatAuto, formatZip, formatTar, formatTarGz:
		return nil
	}
	return fmt.Errorf("invalid format %q (expected auto, zip, tar or tar.gz)", f)
}

// byName returns the format of an archive by the ending of its name; empty if unknown.
func byName(path string) string {
	name := strings.ToLower(filepath.Base(path))
	switch {
	case strings.HasSuffix(name, ".zip"):
		return formatZip
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
		return formatTarGz
	case strings.HasSuffix(name, ".tar"):
		return formatTar
	}
	return ""
}

// byContent returns the format of an archive by its first bytes; empty if unknown.
func byContent(r *bufio.Reader) string {
	head, _ := r.Peek(262)
	switch {
	case bytes.HasPrefix(head, []byte("PK\x03\x04")), bytes.HasPrefix(head, []byte("PK\x05\x06")):
		return formatZip
	case bytes.HasPrefix(head, []byte{0x1f, 0x8b}):
		return formatTarGz
	case len(head) >= 262 && string(head[257:262]) == "ustar":
		return formatTar
	}
	return ""
}

func plural(n int, word string) string {
	switch {
	case n == 1:
		return word
	case strings.HasSuffix(word, "y"):
		return word[:len(word)-1] + "ies"
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

// ctxReader stops reading when the run is cancelled.
type ctxReader struct {
	done func() error
	r    io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.done(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
