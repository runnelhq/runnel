package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// core.split-field splits the text of a field at a separator: into several fields of the
// record ("Weber, Anna" → nachname, vorname), or into one record per part ("a;b;c" →
// three records).
var splitFieldDefinition = transform.Definition{
	Parallel:    true,
	Type:        "core.split-field",
	Category:    transform.CategoryTransform,
	Label:       "Split Field",
	Description: "Splits the text of a field into several fields or into one record per part",
	Example:     "field: name\nseparator: \", \"\ninto: [lastName, firstName]\n",
	Config:      splitField{},
	New:         newSplitField,
}

type splitField struct {
	Field      string   `json:"field" doc:"Field whose text is split" schema:"required,format=runnel-field,example=name"`
	Separator  string   `json:"separator" doc:"Text between the parts, e.g. ; or , (with the space)" schema:"required,example=;"`
	Into       []string `json:"into" doc:"Fields for the parts, in order; more parts than fields stay together in the last one, missing parts are empty. Not with rows"`
	Rows       bool     `json:"rows" doc:"One record per part instead: the field holds the part, the other fields are copied"`
	IndexField string   `json:"indexField" doc:"With rows: field for the position of the part, starting at 1"`
	KeepEmpty  bool     `json:"keepEmpty" doc:"With rows: also a record for empty parts (a;;b); a record whose field is empty is always passed on once" schema:"default=false"`
	Trim       bool     `json:"trim" doc:"Removes spaces around each part" schema:"default=false"`
}

func newSplitField(with map[string]any) (transform.Transform, error) {
	t := &splitField{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	switch {
	case t.Field == "":
		return nil, errors.New("field: name the field whose text is split")
	case t.Separator == "":
		return nil, errors.New("separator: give the text between the parts")
	case t.Rows && len(t.Into) > 0:
		return nil, errors.New("into and rows: choose one – fields for the parts or one record per part")
	case !t.Rows && len(t.Into) == 0:
		return nil, errors.New("into: list the fields for the parts, or set rows: true for one record per part")
	case !t.Rows && (t.IndexField != "" || t.KeepEmpty):
		return nil, errors.New("indexField and keepEmpty apply only with rows: true")
	}
	for i, f := range t.Into {
		if f == "" {
			return nil, fmt.Errorf("into[%d]: empty field name", i)
		}
	}
	return t, nil
}

// Init checks the field and adds the fields of the parts (text) and the position (int).
func (t *splitField) Init(ic *transform.InitContext) (record.Schema, error) {
	ic.Use(t.Field, "field")
	s := ic.In
	if t.Rows {
		s = s.With(t.Field, record.String)
		if t.IndexField != "" {
			s = s.With(t.IndexField, record.Int)
		}
		return s, nil
	}
	for _, f := range t.Into {
		s = s.With(f, record.String)
	}
	return s, nil
}

func (t *splitField) part(p string) string {
	if t.Trim {
		return strings.TrimSpace(p)
	}
	return p
}

func (t *splitField) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	for b := range in {
		for _, r := range b {
			v, _ := r.Get(t.Field)
			text := expr.Format(v)
			var err error
			if t.Rows {
				err = t.splitRows(r, v, text, out)
			} else {
				parts := strings.SplitN(text, t.Separator, len(t.Into))
				if v == nil || text == "" {
					parts = nil
				}
				for i, f := range t.Into {
					if i < len(parts) {
						r.Set(f, t.part(parts[i]))
					} else {
						r.Set(f, nil)
					}
				}
				err = out.Emit(r)
			}
			if err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}

// splitRows passes one record per part; a record with an empty field (or only empty
// parts) is passed on once with the field empty, so that no record gets lost.
func (t *splitField) splitRows(r record.Record, v any, text string, out *transform.Output) error {
	var parts []string
	if v != nil && text != "" {
		for _, p := range strings.Split(text, t.Separator) {
			if p = t.part(p); p != "" || t.KeepEmpty {
				parts = append(parts, p)
			}
		}
	}
	if len(parts) == 0 {
		r.Set(t.Field, nil)
		if t.IndexField != "" {
			r.Set(t.IndexField, nil)
		}
		return out.Emit(r)
	}
	for i, p := range parts {
		c := r
		if i < len(parts)-1 {
			c = r.Clone()
		}
		c.Set(t.Field, p)
		if t.IndexField != "" {
			c.Set(t.IndexField, int64(i+1))
		}
		if err := out.Emit(c); err != nil {
			return err
		}
	}
	return nil
}
