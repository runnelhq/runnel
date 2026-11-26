package csv

import (
	"cmp"
	"context"
	stdcsv "encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// csv.output writes records to a CSV file and passes them on.
var outputDefinition = transform.Definition{
	Type:         "csv.output",
	RecordErrors: true,
	Category:     transform.CategoryOutput,
	Label:        "CSV Output",
	Description:  "Writes records to a CSV file and passes them on",
	Example:      "path: out/orders.csv\nfields:\n  - { name: Datum, format: dd.MM.yyyy }\n  - { name: Betrag, format: \"#.##0,00\" }\n",
	Config:       output{},
	New:          newOutput,
	Reference: action.Reference{
		Records: "Passes every written record on unchanged to all data links.",
		Errors:  "A record with a value that cannot be written in its format or encoding goes to the error link and is not written; without one, the pipeline stops. With ifExists: fail, an existing file stops the pipeline before anything is written. Except with ifExists: append, the file is written to a temporary file first and replaces an existing one only once all records are written, so a failure leaves the old file.",
	},
}

type output struct {
	Path      string     `json:"path" doc:"CSV file to write, relative to the project folder or absolute" schema:"required,example=C:\\out\\orders.csv"`
	Delimiter string     `json:"delimiter" doc:"Character between the values, e.g. ; or \\t for tab; empty: comma" schema:"example=;"`
	Header    *bool      `json:"header" doc:"Whether the first row holds the names of the columns" schema:"default=true"`
	Encoding  string     `json:"encoding" doc:"Encoding of the file; utf-8-bom lets Excel recognize umlauts, utf-16 is little endian with byte order mark" schema:"enum=utf-8-bom|utf-8|windows-1252|utf-16,default=utf-8-bom"`
	IfExists  string     `json:"ifExists" doc:"If the file exists: replace it once all records are written, append to it, or fail" schema:"enum=replace|append|fail,default=replace"`
	Fields    []outField `json:"fields" doc:"Columns to write, in this order, with a format; empty: all fields of the records"`

	codecs []codec
	cols   []string // columns known before the run (fields or a closed schema)
	heads  []string // names in the header, with fields (as or name)
}

// outField is a column of csv.output.
type outField struct {
	Name   string `json:"name" doc:"Name of the field to write" schema:"required,format=runnel-field,example=amount"`
	As     string `json:"as" doc:"Name of the column in the header; empty: the name of the field" schema:"example=Betrag"`
	Format string `json:"format" doc:"Number pattern like #.##0,00 or date pattern like dd.MM.yyyy; empty: 1234.5 and ISO dates" schema:"example=dd.MM.yyyy"`
}

func newOutput(with map[string]any) (transform.Transform, error) {
	t := &output{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	seen, heads := map[string]bool{}, map[string]bool{}
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
		head := cmp.Or(f.As, f.Name)
		if heads[head] {
			return nil, fmt.Errorf("fields: field %q: the column %q is written twice; choose another as", f.Name, head)
		}
		heads[head] = true
		t.heads = append(t.heads, head)
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
	file := paths.Resolve(ctx, t.Path)
	f, appending, err := t.open(file)
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
			if rerr := os.Rename(f.Name(), file); rerr != nil {
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
		heads := cols
		if t.heads != nil {
			heads = t.heads
		}
		if err := t.writeRow(cw, heads, check); err != nil {
			return fmt.Errorf("%s: header: %w", t.Path, err)
		}
		return nil
	}
	skipped := map[string]bool{}
	var colCodecs []codec
	var row []string
	var n, seen int64 // records written, records received
	for b := range in {
		for _, r := range b {
			if cols == nil {
				cols = r.Names() // open schema without fields: the first record decides
			}
			if colCodecs == nil {
				colCodecs = make([]codec, len(cols)) // looked up once, not per field and record
				for i, c := range cols {
					colCodecs[i] = codecs[c]
				}
			}
			if err := writeHeader(); err != nil {
				return n, err
			}
			seen++
			row = row[:0]
			var bad error
			var badField string
			for i, c := range cols {
				// Records usually have the columns in this order: GetAt looks there first.
				v, _ := r.GetAt(i, c)
				s, err := colCodecs[i].Format(v)
				if err == nil && check != nil {
					err = check(s)
				}
				if err != nil {
					bad, badField = fmt.Errorf("%s: record %d, field %q: %w", t.Path, seen, c, err), c
					break
				}
				row = append(row, s)
			}
			if bad != nil {
				// Not written: to an error link, or the pipeline stops.
				if err := out.Fail(r, badField, bad); err != nil {
					return n, err
				}
				continue
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
			n++
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

// open opens file (t.Path resolved, ADR 0020) to write into: for replace a temporary file next to the target,
// renamed when all records are written; for append the file itself.
func (t *output) open(file string) (f *os.File, appending bool, err error) {
	if t.Path == "" {
		return nil, false, errors.New("path: no file given")
	}
	info, err := os.Stat(file)
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
			f, err := os.OpenFile(file, os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				return nil, false, fmt.Errorf("cannot write %q: %w", t.Path, plain(err))
			}
			return f, true, nil
		}
	}
	dir, base := filepath.Split(file)
	if dir == "" {
		dir = "."
	}
	f, err = os.CreateTemp(dir, "."+base+".*.tmp")
	if err != nil {
		return nil, false, fmt.Errorf("cannot write %q: %w", t.Path, plain(err))
	}
	return f, false, nil
}
