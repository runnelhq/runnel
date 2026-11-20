package csv

import (
	"context"
	stdcsv "encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// csv.output writes records to a CSV file and passes them on.
var outputDefinition = transform.Definition{
	Type:        "csv.output",
	Category:    transform.CategoryOutput,
	Label:       "CSV Output",
	Description: "Writes records to a CSV file and passes them on",
	Config:      output{},
	New:         newOutput,
}

type output struct {
	Path      string     `json:"path" doc:"CSV file to write; relative paths refer to the working directory of runnel" schema:"required,example=C:\\out\\orders.csv"`
	Delimiter string     `json:"delimiter" doc:"Character between the values, e.g. ; or \\t for tab; empty: comma" schema:"example=;"`
	Header    *bool      `json:"header" doc:"Whether the first row holds the names of the columns" schema:"default=true"`
	Encoding  string     `json:"encoding" doc:"Encoding of the file; utf-8-bom lets Excel recognize umlauts" schema:"enum=utf-8-bom|utf-8|windows-1252,default=utf-8-bom"`
	IfExists  string     `json:"ifExists" doc:"What happens if the file exists: replace it once all records are written, append to it, or fail" schema:"enum=replace|append|fail,default=replace"`
	Fields    []outField `json:"fields" doc:"Columns to write, in this order, with a format; empty: all fields of the records"`

	codecs []codec
	cols   []string // columns known before the run (fields or a closed schema)
}

// outField is a column of csv.output.
type outField struct {
	Name   string `json:"name" doc:"Name of the field to write (and of the column in the header)" schema:"required,example=amount"`
	Format string `json:"format" doc:"Number pattern like #.##0,00 or date pattern like dd.MM.yyyy; empty: 1234.5 and ISO dates" schema:"example=dd.MM.yyyy"`
}

func newOutput(with map[string]any) (transform.Transform, error) {
	t := &output{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, f := range t.Fields {
		if f.Name == "" {
			return nil, errors.New("fields: every field needs a name")
		}
		if seen[f.Name] {
			return nil, fmt.Errorf("fields: the field %q is listed twice", f.Name)
		}
		seen[f.Name] = true
		c, err := outputCodec(f)
		if err != nil {
			return nil, err
		}
		t.codecs = append(t.codecs, c)
		t.cols = append(t.cols, f.Name)
	}
	if !hasVar(t.Delimiter) {
		if _, err := delimiter(t.Delimiter); err != nil {
			return nil, err
		}
	}
	if !hasVar(t.Encoding) {
		if _, _, err := encoder(io.Discard, t.Encoding, false); err != nil {
			return nil, err
		}
	}
	if !hasVar(t.IfExists) {
		if err := checkIfExists(t.IfExists); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// outputCodec writes the values of a column with a number or date pattern.
func outputCodec(f outField) (codec, error) {
	c, err := format.ForText(f.Format)
	if err != nil {
		return c, fmt.Errorf("field %q: %w", f.Name, err)
	}
	return c, nil
}

func checkIfExists(s string) error {
	switch s {
	case "", "replace", "append", "fail":
		return nil
	}
	return fmt.Errorf("invalid ifExists %q (expected replace, append or fail)", s)
}

// Init checks the fields to write against the incoming schema. Without fields: the
// columns are the fields of a closed schema, or else those of the first record.
func (t *output) Init(ic *transform.InitContext) (record.Schema, error) {
	for i, f := range t.Fields {
		ic.Use(f.Name, fmt.Sprintf("fields[%d].name", i))
	}
	if len(t.Fields) == 0 && !ic.In.Open {
		t.cols = ic.In.Names()
	}
	return ic.In, nil
}

func (t *output) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	comma, err := delimiter(t.Delimiter)
	if err != nil {
		return err
	}
	if err := checkIfExists(t.IfExists); err != nil {
		return err
	}
	f, appending, err := t.open()
	if err != nil {
		return err
	}
	n, err := t.write(ctx, f, appending, comma, in, out)
	if cerr := f.Close(); err == nil && cerr != nil {
		err = fmt.Errorf("cannot write %q: %w", t.Path, plain(cerr))
	}
	if !appending {
		// replace: the file is complete, or it is not written at all.
		if err == nil {
			if rerr := os.Rename(f.Name(), t.Path); rerr != nil {
				err = fmt.Errorf("cannot write %q: %w", t.Path, plainLink(rerr))
			}
		}
		if err != nil {
			_ = os.Remove(f.Name())
		}
	}
	if err != nil {
		return err
	}
	out.Logf(levelInfo, "wrote %d %s to %s", n, plural(n, "row"), t.Path)
	return nil
}

// write writes the header and the records to f and passes the records on.
func (t *output) write(ctx context.Context, f io.Writer, appending bool, comma rune, in <-chan transform.Batch, out *transform.Output) (int64, error) {
	w, check, err := encoder(f, t.Encoding, !appending)
	if err != nil {
		return 0, err
	}
	cw := stdcsv.NewWriter(w)
	cw.Comma = comma
	cw.UseCRLF = true // what Windows programs expect

	codecs := map[string]codec{}
	for i, f := range t.Fields {
		codecs[f.Name] = t.codecs[i]
	}
	cols := t.cols
	header := (t.Header == nil || *t.Header) && !appending
	writeHeader := func() error {
		if !header {
			return nil
		}
		header = false
		if err := t.writeRow(cw, cols, check); err != nil {
			return fmt.Errorf("%s: header: %w", t.Path, err)
		}
		return nil
	}
	skipped := map[string]bool{}
	var row []string
	var n int64
	for b := range in {
		for _, r := range b {
			if cols == nil {
				cols = r.Names() // open schema without fields: the first record decides
			}
			if err := writeHeader(); err != nil {
				return n, err
			}
			n++
			row = row[:0]
			for _, c := range cols {
				v, _ := r.Get(c)
				s, err := codecs[c].Format(v)
				if err == nil && check != nil {
					err = check(s)
				}
				if err != nil {
					return n, fmt.Errorf("%s: record %d, field %q: %w", t.Path, n, c, err)
				}
				row = append(row, s)
			}
			if len(t.Fields) == 0 && r.Len() > len(cols) {
				for _, name := range r.Names() {
					if !skipped[name] && !slices.Contains(cols, name) {
						skipped[name] = true
						out.Logf(levelWarn, "the field %q is not written: the first record has no such field; list the columns with fields:", name)
					}
				}
			}
			if err := cw.Write(row); err != nil {
				return n, fmt.Errorf("cannot write %q: %w", t.Path, plain(err))
			}
			if err := out.Emit(r); err != nil {
				return n, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return n, err
	}
	// A file without records gets its header, if the columns are known.
	if cols != nil {
		if err := writeHeader(); err != nil {
			return n, err
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return n, fmt.Errorf("cannot write %q: %w", t.Path, plain(err))
	}
	if c, ok := w.(io.Closer); ok && w != f { // the encoder, not the file
		if err := c.Close(); err != nil {
			return n, fmt.Errorf("cannot write %q: %w", t.Path, plain(err))
		}
	}
	return n, nil
}

func (t *output) writeRow(cw *stdcsv.Writer, row []string, check func(string) error) error {
	if check != nil {
		for _, s := range row {
			if err := check(s); err != nil {
				return err
			}
		}
	}
	return cw.Write(row)
}

// open opens the file to write into: for replace a temporary file next to the target,
// renamed when all records are written; for append the file itself.
func (t *output) open() (f *os.File, appending bool, err error) {
	if t.Path == "" {
		return nil, false, errors.New("path: no file given")
	}
	info, err := os.Stat(t.Path)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, false, fmt.Errorf("cannot write %q: %w", t.Path, plain(err))
	}
	if exists && info.IsDir() {
		return nil, false, fmt.Errorf("cannot write %q: it is a folder", t.Path)
	}
	switch t.IfExists {
	case "fail":
		if exists {
			return nil, false, fmt.Errorf("%q already exists (ifExists: fail)", t.Path)
		}
	case "append":
		if exists && info.Size() > 0 {
			f, err := os.OpenFile(t.Path, os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				return nil, false, fmt.Errorf("cannot write %q: %w", t.Path, plain(err))
			}
			return f, true, nil
		}
	}
	dir, base := filepath.Split(t.Path)
	if dir == "" {
		dir = "."
	}
	f, err = os.CreateTemp(dir, "."+base+".*.tmp")
	if err != nil {
		return nil, false, fmt.Errorf("cannot write %q: %w", t.Path, plain(err))
	}
	return f, false, nil
}
