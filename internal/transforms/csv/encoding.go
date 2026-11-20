package csv

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

// Encodings of csv.input; "auto" recognizes a byte order mark (UTF-8, UTF-16) and
// otherwise reads UTF-8.
const (
	encAuto        = "auto"
	encUTF8        = "utf-8"
	encUTF8BOM     = "utf-8-bom"
	encWindows1252 = "windows-1252"
	encUTF16       = "utf-16"
)

// decoder returns a reader that delivers UTF-8 text without byte order mark.
func decoder(r io.Reader, enc string) (io.Reader, error) {
	br := bufio.NewReader(r)
	switch enc {
	case "", encAuto, encUTF8, encUTF8BOM:
		head, _ := br.Peek(3)
		switch {
		case bytes.HasPrefix(head, []byte{0xEF, 0xBB, 0xBF}):
			_, _ = br.Discard(3)
			return &utf8Check{r: br}, nil
		case enc != encUTF8 && enc != encUTF8BOM && (bytes.HasPrefix(head, []byte{0xFF, 0xFE}) || bytes.HasPrefix(head, []byte{0xFE, 0xFF})):
			return transform.NewReader(br, unicode.UTF16(unicode.LittleEndian, unicode.ExpectBOM).NewDecoder()), nil
		}
		return &utf8Check{r: br}, nil
	case encWindows1252:
		return transform.NewReader(br, charmap.Windows1252.NewDecoder()), nil
	case encUTF16:
		// The byte order mark decides; without one, little endian as written by Windows.
		return transform.NewReader(br, unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder()), nil
	}
	return nil, fmt.Errorf("invalid encoding %q (expected auto, utf-8, windows-1252 or utf-16)", enc)
}

// errNotUTF8 suggests the encoding Windows programs often use.
var errNotUTF8 = errors.New("the file is not valid UTF-8; if it comes from a Windows program, set encoding: windows-1252")

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
			c.err = errNotUTF8
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

// encoder returns a writer that writes text in an encoding of csv.output and a check
// for values it cannot represent (nil: all can be written). With bom, utf-8-bom starts
// with a byte order mark: Excel recognizes umlauts only with it. Close the writer, if it
// is an io.Closer, to write the rest.
func encoder(w io.Writer, enc string, bom bool) (io.Writer, func(string) error, error) {
	switch enc {
	case "", encUTF8BOM:
		if bom {
			if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
				return nil, nil, err
			}
		}
		return w, nil, nil
	case encUTF8:
		return w, nil, nil
	case encWindows1252:
		return transform.NewWriter(w, charmap.Windows1252.NewEncoder()), windows1252, nil
	}
	return nil, nil, fmt.Errorf("invalid encoding %q (expected utf-8-bom, utf-8 or windows-1252)", enc)
}

// windows1252 rejects text with characters Windows-1252 has not, instead of writing
// replacements.
func windows1252(s string) error {
	for _, r := range s {
		if r >= utf8.RuneSelf {
			if _, ok := charmap.Windows1252.EncodeRune(r); !ok {
				return fmt.Errorf("the character %q cannot be written in windows-1252; use encoding: utf-8-bom", r)
			}
		}
	}
	return nil
}
