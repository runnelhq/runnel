package csv

import (
	"bufio"
	"bytes"
	"context"
	stdcsv "encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/transform"
)

// sampleValues is the number of values per field shown with a proposal.
const sampleValues = 5

// detectFields proposes the fields of csv.input from the first rows of its file (S46):
// the names from the header (else field1, field2 …), a type and format per column from
// its values (format.Propose).
func detectFields(ctx context.Context, with map[string]any, limit int) (*transform.Detection, error) {
	t := &input{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	comma, err := delimiter(t.Delimiter)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(paths.Resolve(ctx, t.Path))
	if err != nil {
		return nil, fmt.Errorf("cannot read %q: %w", t.Path, plain(err))
	}
	defer func() { _ = f.Close() }()
	br := bufio.NewReader(f)
	head, _ := br.Peek(3)
	text, err := decoder(br, t.Encoding)
	if err != nil {
		return nil, err
	}
	r := stdcsv.NewReader(text)
	r.Comma = comma
	r.FieldsPerRecord = -1 // rows with more or fewer values are still a sample

	header := t.Header == nil || *t.Header
	d := &transform.Detection{ByPosition: !header, Source: []transform.Fact{
		{Name: "File", Value: t.Path},
		{Name: "Delimiter", Value: delimiterName(comma)},
		{Name: "Header", Value: map[bool]string{true: "first row", false: "none"}[header]},
		{Name: "Encoding", Value: encodingName(t.Encoding, head)},
	}}
	var names []string
	var columns [][]string // values per column
	var lines []int        // line of each row
	for {
		row, err := r.Read()
		if errors.Is(err, io.EOF) {
			d.Complete = true
			break
		}
		if err != nil {
			return nil, t.readError(err)
		}
		if names == nil {
			if names, err = columnNames(row, header, t.Path); err != nil {
				return nil, err
			}
			columns = make([][]string, len(names))
			if header {
				continue
			}
		}
		if d.Rows == limit {
			break // more rows than read
		}
		d.Rows++
		line, _ := r.FieldPos(0)
		lines = append(lines, line)
		for i := range names {
			columns[i] = append(columns[i], value(row, i))
		}
	}
	for i, name := range names {
		p := format.Propose(columns[i], lines)
		values := columns[i][:min(len(columns[i]), sampleValues)]
		d.Fields = append(d.Fields, transform.DetectedField{Name: name, Type: string(p.Type), Format: p.Format,
			Values: values, Note: p.Note, Warning: p.Warning})
	}
	if err := transform.MergeFields(d, with["fields"]); err != nil {
		return nil, err
	}
	return d, nil
}

// columnNames returns the names of the columns: from the header, else field1, field2 …
// (as csv.input reads them without fields).
func columnNames(first []string, header bool, path string) ([]string, error) {
	names := make([]string, len(first))
	seen := map[string]bool{}
	for i, h := range first {
		name := "field" + strconv.Itoa(i+1)
		if header && strings.TrimSpace(h) != "" {
			name = strings.TrimSpace(h)
		}
		if seen[name] {
			return nil, fmt.Errorf("%s:1: the column %q appears twice in the header", path, name)
		}
		seen[name] = true
		names[i] = name
	}
	return names, nil
}

func delimiterName(r rune) string {
	if r == '\t' {
		return "tab"
	}
	return string(r)
}

// encodingName describes the encoding the file is read with; auto by its byte order mark.
func encodingName(enc string, head []byte) string {
	switch {
	case enc != "" && enc != encAuto:
		return enc
	case bytes.HasPrefix(head, []byte{0xEF, 0xBB, 0xBF}):
		return "UTF-8 (byte order mark)"
	case bytes.HasPrefix(head, []byte{0xFF, 0xFE}) || bytes.HasPrefix(head, []byte{0xFE, 0xFF}):
		return "UTF-16 (byte order mark)"
	}
	return "UTF-8"
}
