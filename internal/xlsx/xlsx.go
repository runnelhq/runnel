// Package xlsx reads Excel workbooks (.xlsx, .xlsm) for excel.input (roadmap S67a): the
// sheets of the workbook, then the rows of one sheet as a stream, with the type of each
// cell (text, number, true/false, date) and errors like #DIV/0!. Formulas give the value
// Excel calculated last; nothing is calculated here.
//
// A workbook is a ZIP archive of XML parts. Every part is read with a size limit, so a
// small file cannot unpack into gigabytes (ZIP bomb); the rows of a sheet are read one
// after another, so the memory stays small even for large sheets. The old binary format
// (.xls), binary workbooks (.xlsb) and workbooks protected with a password are rejected
// with a clear message.
package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
)

// Limits for the parts of a workbook, unpacked. Sheets are read as a stream; the other
// parts are held in memory. Variables for tests.
var (
	maxSheetSize   int64 = 4 << 30   // XML of one sheet
	maxStringsSize int64 = 256 << 20 // shared strings
	maxPartSize    int64 = 32 << 20  // workbook, relationships, styles
)

// The size of a sheet in Excel.
const (
	maxColumns = 16384 // XFD
	maxRows    = 1048576
)

// ErrOldFormat is returned for files in the binary format of Excel 97–2003 (.xls). Excel
// also stores workbooks protected with a password in that container.
var ErrOldFormat = errors.New("the file is an Excel 97–2003 workbook (.xls) or protected with a password; save it in Excel as .xlsx without a password")

// Book is an open workbook.
type Book struct {
	zr       *zip.Reader
	closer   io.Closer
	files    map[string]*zip.File // by lowercase name
	sheets   []Sheet
	strings  []string
	formats  []kind // by the number of the cell format (s="…")
	date1904 bool
}

// Sheet is a sheet of the workbook, in the order of the tabs.
type Sheet struct {
	Name   string
	Hidden bool
	part   string
}

// Open opens the workbook in a file.
func Open(name string) (*Book, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err == nil {
		var b *Book
		if b, err = OpenReader(f, st.Size()); err == nil {
			b.closer = f
			return b, nil
		}
	}
	_ = f.Close()
	return nil, err
}

// OpenReader opens a workbook from r with the given size. The caller closes r after
// the book.
func OpenReader(r io.ReaderAt, size int64) (*Book, error) {
	head := make([]byte, 8)
	if n, _ := r.ReadAt(head, 0); n == 8 && bytes.Equal(head, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}) {
		return nil, ErrOldFormat
	}
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, errors.New("the file is not an Excel workbook (.xlsx)")
	}
	b := &Book{zr: zr, files: map[string]*zip.File{}}
	for _, f := range zr.File {
		b.files[strings.ToLower(strings.ReplaceAll(f.Name, `\`, "/"))] = f
	}
	if err := b.load(); err != nil {
		return nil, err
	}
	return b, nil
}

// Close closes the file of the workbook.
func (b *Book) Close() error {
	if b.closer != nil {
		return b.closer.Close()
	}
	return nil
}

// Sheets returns the sheets in the order of the tabs.
func (b *Book) Sheets() []Sheet { return b.sheets }

// Date1904 reports the date system of Macs before Excel 2011 (day 0 is 1904-01-01).
func (b *Book) Date1904() bool { return b.date1904 }

// load reads the workbook: its sheets, the shared strings and the cell formats.
func (b *Book) load() error {
	root, err := b.rels("_rels/.rels", "")
	if err != nil {
		return err
	}
	main := root.target("/officeDocument")
	if main == "" {
		return errors.New("the file is not an Excel workbook (.xlsx): no workbook in it")
	}
	if strings.HasSuffix(strings.ToLower(main), ".bin") {
		return errors.New("binary workbooks (.xlsb) are not supported; save the file in Excel as .xlsx")
	}
	rels, err := b.rels(partRels(main), path.Dir(main))
	if err != nil {
		return err
	}
	if err := b.workbook(main, rels); err != nil {
		return err
	}
	if p := rels.target("/sharedStrings"); p != "" {
		if b.strings, err = b.sharedStrings(p); err != nil {
			return err
		}
	}
	if p := rels.target("/styles"); p != "" {
		if b.formats, err = b.styles(p); err != nil {
			return err
		}
	}
	return nil
}

// open opens a part with a limit for its size.
func (b *Book) open(name string, limit int64) (io.ReadCloser, error) {
	f := b.files[strings.ToLower(name)]
	if f == nil {
		return nil, fmt.Errorf("the workbook is damaged: %s is missing", name)
	}
	if f.UncompressedSize64 > uint64(limit) { //nolint:gosec // the limits are positive
		return nil, fmt.Errorf("%s is too large (%s unpacked, at most %s)", name, size(int64(min(f.UncompressedSize64, 1<<62))), size(limit))
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("the workbook is damaged: %s: %w", name, err)
	}
	// The size in the archive can be wrong: the reader stops at the limit anyway.
	return &limited{r: io.LimitReader(rc, limit+1), c: rc, name: name, limit: limit}, nil
}

// limited fails when a part unpacks to more than its limit.
type limited struct {
	r     io.Reader
	c     io.Closer
	name  string
	limit int64
	n     int64
}

func (l *limited) Read(p []byte) (int, error) {
	n, err := l.r.Read(p)
	l.n += int64(n)
	if l.n > l.limit {
		return n, fmt.Errorf("%s is too large (more than %s unpacked)", l.name, size(l.limit))
	}
	return n, err
}

func (l *limited) Close() error { return l.c.Close() }

func size(n int64) string {
	switch {
	case n >= 1<<30:
		return strconv.FormatInt(n>>30, 10) + " GB"
	case n >= 1<<20:
		return strconv.FormatInt(n>>20, 10) + " MB"
	}
	return strconv.FormatInt(n, 10) + " bytes"
}

// relations are the relationships of a part: target by id and type.
type relations []struct{ id, typ, target string }

func (r relations) target(typeSuffix string) string {
	for _, x := range r {
		if strings.HasSuffix(x.typ, typeSuffix) {
			return x.target
		}
	}
	return ""
}

func (r relations) byID(id string) string {
	for _, x := range r {
		if x.id == id {
			return x.target
		}
	}
	return ""
}

// partRels is the name of the relationships of a part, e.g. xl/_rels/workbook.xml.rels.
func partRels(part string) string { return path.Join(path.Dir(part), "_rels", path.Base(part)+".rels") }

// rels reads relationships; targets are resolved against dir.
func (b *Book) rels(name, dir string) (relations, error) {
	var out relations
	err := b.parse(name, maxPartSize, func(_ *xml.Decoder, t xml.StartElement) error {
		if t.Name.Local == "Relationship" && attr(t, "TargetMode") != "External" {
			target := strings.ReplaceAll(attr(t, "Target"), `\`, "/")
			if abs, ok := strings.CutPrefix(target, "/"); ok {
				target = abs
			} else {
				target = path.Join(dir, target)
			}
			out = append(out, struct{ id, typ, target string }{attr(t, "Id"), attr(t, "Type"), target})
		}
		return nil
	})
	return out, err
}

func (b *Book) workbook(name string, rels relations) error {
	err := b.parse(name, maxPartSize, func(_ *xml.Decoder, t xml.StartElement) error {
		switch t.Name.Local {
		case "workbookPr":
			v := attr(t, "date1904")
			b.date1904 = v == "1" || v == "true"
		case "sheet":
			id := ""
			for _, a := range t.Attr {
				if a.Name.Local == "id" && a.Name.Space != "" {
					id = a.Value
				}
			}
			s := Sheet{Name: attr(t, "name"), Hidden: attr(t, "state") != "" && attr(t, "state") != "visible", part: rels.byID(id)}
			b.sheets = append(b.sheets, s)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(b.sheets) == 0 {
		return errors.New("the workbook has no sheets")
	}
	return nil
}

// sharedStrings reads the texts that the cells refer to by number.
func (b *Book) sharedStrings(name string) ([]string, error) {
	var out []string
	err := b.parse(name, maxStringsSize, func(d *xml.Decoder, t xml.StartElement) error {
		if t.Name.Local != "si" {
			return nil
		}
		s, err := richText(d, "si")
		out = append(out, s)
		return err
	})
	return out, err
}

// styles reads which cell formats show dates and times.
func (b *Book) styles(name string) ([]kind, error) {
	custom := map[int]kind{}
	var out []kind
	inXfs := false
	err := b.parse(name, maxPartSize, func(_ *xml.Decoder, t xml.StartElement) error {
		switch t.Name.Local {
		case "numFmt":
			if id, err := strconv.Atoi(attr(t, "numFmtId")); err == nil {
				custom[id] = kindOfCode(attr(t, "formatCode"))
			}
		case "cellXfs":
			inXfs = true
		case "cellStyleXfs", "dxfs":
			inXfs = false
		case "xf":
			if inXfs {
				id, _ := strconv.Atoi(attr(t, "numFmtId"))
				k, ok := custom[id]
				if !ok {
					k = builtin(id)
				}
				out = append(out, k)
			}
		}
		return nil
	})
	return out, err
}

// parse reads a part and calls fn for every start element; fn may read on with d.
func (b *Book) parse(name string, limit int64, fn func(d *xml.Decoder, t xml.StartElement) error) error {
	rc, err := b.open(name, limit)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	d := xml.NewDecoder(rc)
	for {
		tok, err := d.RawToken()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return xmlError(name, err)
		}
		if t, ok := tok.(xml.StartElement); ok {
			if err := fn(d, t); err != nil {
				return xmlError(name, err)
			}
		}
	}
}

func xmlError(part string, err error) error {
	if se, ok := errors.AsType[*xml.SyntaxError](err); ok {
		return fmt.Errorf("the workbook is damaged: %s, line %d: %s", part, se.Line, se.Msg)
	}
	if strings.Contains(err.Error(), "too large") {
		return err
	}
	return fmt.Errorf("the workbook is damaged: %s: %w", part, err)
}

func attr(t xml.StartElement, name string) string {
	for _, a := range t.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// richText reads the text of an element up to its end: the <t> elements, also inside
// runs of formatted text (<r>), without phonetic hints (<rPh>).
func richText(d *xml.Decoder, end string) (string, error) {
	var sb strings.Builder
	inT, skip := false, 0
	for {
		tok, err := d.RawToken()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inT = true
			case "rPh":
				skip++
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inT = false
			case "rPh":
				skip--
			case end:
				return unescape(sb.String()), nil
			}
		case xml.CharData:
			if inT && skip == 0 {
				sb.Write(t)
			}
		}
	}
}

// unescape replaces the escapes of OOXML for characters XML cannot hold, e.g.
// _x000D_ for a carriage return; _x005F_ escapes the underscore itself.
func unescape(s string) string {
	if !strings.Contains(s, "_x") {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); {
		if i+7 <= len(s) && s[i] == '_' && s[i+1] == 'x' && s[i+6] == '_' {
			if n, err := strconv.ParseUint(s[i+2:i+6], 16, 16); err == nil {
				sb.WriteRune(rune(n))
				i += 7
				continue
			}
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String()
}
