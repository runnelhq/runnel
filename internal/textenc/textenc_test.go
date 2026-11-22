package textenc

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
	"unicode/utf8"
)

// read decodes data with the encoding; one byte per read call when slow is set, so that
// characters are split between chunks.
func read(t *testing.T, data []byte, enc string, slow bool) (string, error) {
	t.Helper()
	var r io.Reader = bytes.NewReader(data)
	if slow {
		r = iotest.OneByteReader(r)
	}
	d, err := Decoder(r, enc)
	if err != nil {
		t.Fatalf("Decoder(%q): %v", enc, err)
	}
	out, err := io.ReadAll(d)
	return string(out), err
}

func utf16(order string, s string, bom bool) []byte {
	var b []byte
	if bom {
		if order == "le" {
			b = append(b, 0xFF, 0xFE)
		} else {
			b = append(b, 0xFE, 0xFF)
		}
	}
	for _, r := range s { // only characters of the Basic Multilingual Plane in these tests
		hi, lo := byte(r>>8), byte(r)
		if order == "le" {
			b = append(b, lo, hi)
		} else {
			b = append(b, hi, lo)
		}
	}
	return b
}

func TestDecoder(t *testing.T) {
	const text = "Kunde;Straße;Betrag\r\nMüller;Hauptstraße 1;12,50 €\r\n"
	bom := []byte{0xEF, 0xBB, 0xBF}
	tests := []struct {
		name string
		data []byte
		enc  string
	}{
		{"utf-8 auto", []byte(text), Auto},
		{"utf-8 empty encoding", []byte(text), ""},
		{"utf-8 with bom auto", append(bom, text...), Auto},
		{"utf-8 with bom explicit", append(bom, text...), UTF8BOM},
		{"utf-8 explicit with bom", append(bom, text...), UTF8},
		{"utf-16 le auto", utf16("le", text, true), Auto},
		{"utf-16 be auto", utf16("be", text, true), Auto},
		{"utf-16 le explicit", utf16("le", text, true), UTF16},
		{"utf-16 be explicit", utf16("be", text, true), UTF16},
		{"utf-16 without bom is little endian", utf16("le", text, false), UTF16},
		{"windows-1252", []byte("Kunde;Stra\xDFe;Betrag\r\nM\xFCller;Hauptstra\xDFe 1;12,50 \x80\r\n"), Windows1252},
	}
	for _, tt := range tests {
		for _, slow := range []bool{false, true} {
			got, err := read(t, tt.data, tt.enc, slow)
			if err != nil || got != text {
				t.Errorf("%s (slow %v): %q, %v; want %q", tt.name, slow, got, err, text)
			}
		}
	}
}

// Text that is not UTF-8 is an error with a hint, wherever the broken byte is: at the
// start, in the middle, split between reads or as an incomplete character at the end.
func TestDecoderNotUTF8(t *testing.T) {
	for _, data := range []string{
		"\x80abc",
		"Müller;" + "\xFC" + "ber",                // windows-1252 ü
		strings.Repeat("a", 8190) + "\xC3\x28",    // broken pair across the chunk size
		strings.Repeat("a", 8191) + "€"[:2] + "x", // incomplete € split by the chunk
		"ok " + "€"[:2],                           // incomplete character at the end
		string(utf16("le", "Kunde", true)),        // UTF-16 read as UTF-8
	} {
		for _, slow := range []bool{false, true} {
			_, err := read(t, []byte(data), UTF8, slow)
			if !errors.Is(err, ErrNotUTF8) {
				t.Errorf("%q (slow %v): err = %v, want ErrNotUTF8", shorten(data), slow, err)
			}
		}
	}
}

// Valid characters on the border between two chunks of the reader are kept whole.
func TestDecoderChunkBorder(t *testing.T) {
	for pad := 8185; pad <= 8192; pad++ {
		text := strings.Repeat("a", pad) + "€ü𝄞" + strings.Repeat("b", 10)
		got, err := read(t, []byte(text), Auto, false)
		if err != nil || got != text {
			t.Errorf("pad %d: err %v, equal %v", pad, err, got == text)
		}
	}
}

func TestDecoderInvalidEncoding(t *testing.T) {
	_, err := Decoder(strings.NewReader(""), "latin-9")
	if err == nil || !strings.Contains(err.Error(), `invalid encoding "latin-9"`) {
		t.Errorf("err = %v", err)
	}
}

// Errors of the underlying reader are passed on after the text read before them.
func TestDecoderReadError(t *testing.T) {
	boom := errors.New("network drive gone")
	d, err := Decoder(io.MultiReader(strings.NewReader("abc"), iotest.ErrReader(boom)), Auto)
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(d)
	if string(out) != "abc" || !errors.Is(err, boom) {
		t.Errorf("%q, %v", out, err)
	}
}

// FuzzDecoder: reading as UTF-8 either returns the input unchanged (without a leading
// byte order mark) or fails with ErrNotUTF8 exactly when the input is not valid UTF-8,
// however the reads are split.
func FuzzDecoder(f *testing.F) {
	for _, s := range []string{"", "abc", "\xEF\xBB\xBFabc", "Straße €", "\x80", "ok \xE2\x82"} {
		f.Add([]byte(s), false)
	}
	f.Fuzz(func(t *testing.T, data []byte, slow bool) {
		var r io.Reader = bytes.NewReader(data)
		if slow {
			r = iotest.OneByteReader(r)
		}
		d, err := Decoder(r, UTF8)
		if err != nil {
			t.Fatal(err)
		}
		out, err := io.ReadAll(d)
		want := bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
		switch {
		case utf8.Valid(want) && (err != nil || !bytes.Equal(out, want)):
			t.Fatalf("valid input: %q, %v", out, err)
		case !utf8.Valid(want) && !errors.Is(err, ErrNotUTF8):
			t.Fatalf("invalid input accepted: %v", err)
		}
	})
}

func shorten(s string) string {
	if len(s) > 40 {
		return s[:20] + "…" + s[len(s)-20:]
	}
	return s
}
