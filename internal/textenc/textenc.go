// Package textenc reads text files in the encodings of Windows: UTF-8 with or without
// byte order mark, UTF-16 (recognized by its byte order mark) and Windows-1252. Shared
// by the input transforms (csv.input, json.input).
package textenc

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// Encodings of input files; "auto" recognizes a byte order mark (UTF-8, UTF-16) and
// otherwise reads UTF-8.
const (
	Auto        = "auto"
	UTF8        = "utf-8"
	UTF8BOM     = "utf-8-bom"
	Windows1252 = "windows-1252"
	UTF16       = "utf-16"
)

// Decoder returns a reader that delivers UTF-8 text without byte order mark.
func Decoder(r io.Reader, enc string) (io.Reader, error) {
	br := bufio.NewReader(r)
	switch enc {
	case "", Auto, UTF8, UTF8BOM:
		head, _ := br.Peek(3)
		switch {
		case bytes.HasPrefix(head, []byte{0xEF, 0xBB, 0xBF}):
			_, _ = br.Discard(3)
			return &utf8Check{r: br}, nil
		case enc != UTF8 && enc != UTF8BOM && (bytes.HasPrefix(head, []byte{0xFF, 0xFE}) || bytes.HasPrefix(head, []byte{0xFE, 0xFF})):
			return transform.NewReader(br, unicode.UTF16(unicode.LittleEndian, unicode.ExpectBOM).NewDecoder()), nil
		}
		return &utf8Check{r: br}, nil
	case Windows1252:
		return transform.NewReader(br, charmap.Windows1252.NewDecoder()), nil
	case UTF16:
		// The byte order mark decides; without one, little endian as written by Windows.
		return transform.NewReader(br, unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder()), nil
	}
	return nil, fmt.Errorf("invalid encoding %q (expected auto, utf-8, windows-1252 or utf-16)", enc)
}

// ErrNotUTF8 suggests the encoding Windows programs often use.
var ErrNotUTF8 = errors.New("the file is not valid UTF-8; if it comes from a Windows program, set encoding: windows-1252")

// utf8Check reports text that is not valid UTF-8 instead of passing on broken characters.
type utf8Check struct {
	r       io.Reader
	buf     [8192]byte
	pending []byte // checked text not yet read
	tail    []byte // incomplete character at the end of the last chunk
	err     error
}

func (c *utf8Check) Read(p []byte) (int, error) {
	for len(c.pending) == 0 {
		if c.err != nil {
			return 0, c.err
		}
		n := copy(c.buf[:], c.tail)
		m, err := c.r.Read(c.buf[n:])
		data := c.buf[:n+m]
		// Keep an incomplete character at the end for the next chunk.
		cut := len(data)
		if err == nil {
			for i := len(data) - 1; i >= 0 && i >= len(data)-utf8.UTFMax; i-- {
				if utf8.RuneStart(data[i]) {
					if !utf8.FullRune(data[i:]) {
						cut = i
					}
					break
				}
			}
		}
		if !utf8.Valid(data[:cut]) {
			c.err = ErrNotUTF8
			return 0, c.err
		}
		c.tail = append(c.tail[:0], data[cut:]...)
		c.pending = data[:cut]
		c.err = err
	}
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}
