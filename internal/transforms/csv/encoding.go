package csv

import (
	"fmt"
	"io"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"

	"github.com/runnelhq/runnel/internal/textenc"
)

// Encodings of csv.input and csv.output (package textenc reads them).
const (
	encAuto        = textenc.Auto
	encUTF8        = textenc.UTF8
	encUTF8BOM     = textenc.UTF8BOM
	encWindows1252 = textenc.Windows1252
)

// decoder returns a reader that delivers UTF-8 text without byte order mark.
func decoder(r io.Reader, enc string) (io.Reader, error) { return textenc.Decoder(r, enc) }

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
