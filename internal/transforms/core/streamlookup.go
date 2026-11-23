package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// core.stream-lookup adds the fields of the matching record of the lookup data to each
// record, like db.lookup, with the lookup data coming over an info link (S23b). It reads
// the info link completely into memory first, up to maxRecords records. Keys are compared
// as text (1 matches "1"), so that numbers from CSV files without types match.
var streamLookupDefinition = transform.Definition{
	Type:         "core.stream-lookup",
	Category:     transform.CategoryCombine,
	Label:        "Stream Lookup",
	Description:  "Adds the fields of the matching lookup record to each record; the lookup data comes over an info link",
	Example:      "keys:\n  - { field: customerId, lookupField: id }\nreturn:\n  - { lookupField: name, as: customerName }\n",
	Config:       streamLookup{},
	New:          newStreamLookup,
	RecordErrors: true,
	InfoInput:    true,
}

// defaultMaxLookupRecords is the default of maxRecords (mockup S23b).
const defaultMaxLookupRecords = 100000

type streamLookup struct {
	Keys       []streamLookupKey    `json:"keys" doc:"Fields of the record and the fields of the lookup data they must be equal to, compared as text" schema:"required"`
	Return     []streamLookupReturn `json:"return" doc:"Fields of the matching lookup record added to the record" schema:"required"`
	NotFound   string               `json:"notFound" doc:"Records without a matching lookup record: keep them with the defaults, drop them, or treat them as errors (error link)" schema:"enum=keep|drop|error,default=keep"`
	Multiple   string               `json:"multiple" doc:"Several matching lookup records: take the first in the order they arrived, or treat the record as an error" schema:"enum=first|error,default=first"`
	MaxRecords *int                 `json:"maxRecords" doc:"Most records of the lookup data held in memory; more stop the pipeline (for larger tables use db.lookup)" schema:"default=100000"`
}

type streamLookupKey struct {
	Field       string `json:"field" doc:"Field of the record" schema:"required,format=runnel-field,example=customerId"`
	LookupField string `json:"lookupField" doc:"Field of the lookup data; empty: the same name" schema:"example=id"`
}

type streamLookupReturn struct {
	LookupField string `json:"lookupField" doc:"Field of the lookup data" schema:"required,example=name"`
	As          string `json:"as" doc:"Field for the value; empty: the name of the lookup field"`
	Default     any    `json:"default" doc:"Value when no lookup record matches; missing: an empty value"`
}

func newStreamLookup(with map[string]any) (transform.Transform, error) {
	t := &streamLookup{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	if len(t.Keys) == 0 {
		return nil, errors.New("keys: name at least one field and lookup field to compare")
	}
	if len(t.Return) == 0 {
		return nil, errors.New("return: name at least one field of the lookup data to add")
	}
	for i := range t.Keys {
		if t.Keys[i].Field == "" {
			return nil, fmt.Errorf("keys[%d]: field is empty", i)
		}
		if t.Keys[i].LookupField == "" {
			t.Keys[i].LookupField = t.Keys[i].Field
		}
	}
	for i := range t.Return {
		r := &t.Return[i]
		if r.LookupField == "" {
			return nil, fmt.Errorf("return[%d]: lookupField is empty", i)
		}
		if r.As == "" {
			r.As = r.LookupField
		}
		v, err := record.Normalize(r.Default)
		if err != nil {
			return nil, fmt.Errorf("return[%d] (%s): default: %w", i, r.LookupField, err)
		}
		r.Default = v
	}
	switch t.NotFound {
	case "", "keep", "drop", "error":
	default:
		return nil, fmt.Errorf("invalid notFound %q (expected keep, drop or error)", t.NotFound)
	}
	switch t.Multiple {
	case "", "first", "error":
	default:
		return nil, fmt.Errorf("invalid multiple %q (expected first or error)", t.Multiple)
	}
	if t.MaxRecords != nil && *t.MaxRecords < 1 {
		return nil, errors.New("maxRecords must be at least 1")
	}
	return t, nil
}

// Init checks the key fields in the records and in the lookup data, and adds the returned
// fields with the type they have in the lookup data (or of their default).
func (t *streamLookup) Init(ic *transform.InitContext) (record.Schema, error) {
	for i, k := range t.Keys {
		ic.Use(k.Field, fmt.Sprintf("keys[%d].field", i))
		ic.UseInfo(k.LookupField, fmt.Sprintf("keys[%d].lookupField", i))
	}
	s := ic.In
	for i, r := range t.Return {
		ic.UseInfo(r.LookupField, fmt.Sprintf("return[%d].lookupField", i))
		typ := record.TypeOf(r.Default)
		if ic.Info != nil {
			if f, ok := ic.Info.Field(r.LookupField); ok && (r.Default == nil || f.Type == typ) {
				typ = f.Type
			}
		}
		s = s.With(r.As, typ)
	}
	return s, nil
}

// lookupEntry holds the returned values of the first lookup record of a key and whether
// more had the same key.
type lookupEntry struct {
	values   []any
	multiple bool
}

func (t *streamLookup) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, from := out.Info()
	if info == nil {
		return errors.New("core.stream-lookup needs an info link with the lookup data")
	}
	table, loaded, err := t.load(ctx, info, from, out)
	if err != nil {
		return err
	}
	out.Logf(slog.LevelInfo, "loaded %d lookup %s from %s (%d %s)", loaded, plural(loaded, "record", "records"), from, len(table), plural(len(table), "key", "keys"))

	var read, found, missing int
	for b := range in {
		for _, r := range b {
			read++
			id, ok := t.key(r, func(k streamLookupKey) string { return k.Field })
			e := table[id]
			if !ok {
				e = nil // an empty key matches nothing
			}
			var problem string
			switch {
			case e == nil && t.NotFound == "drop":
				missing++
				continue
			case e == nil && t.NotFound == "error":
				problem = "no lookup record from " + from + " matches"
			case e != nil && e.multiple && t.Multiple == "error":
				problem = "several lookup records from " + from + " match"
			}
			if problem != "" {
				if e == nil {
					missing++
				}
				if err := out.Fail(r, t.Keys[0].Field, fmt.Errorf("record %d: %s %s", read, problem, t.describe(r))); err != nil {
					return err
				}
				continue
			}
			if e == nil {
				missing++
			} else {
				found++
			}
			for i, rt := range t.Return {
				v := rt.Default
				if e != nil {
					v = e.values[i]
				}
				r.Set(rt.As, v)
			}
			if err := out.Emit(r); err != nil {
				return err
			}
		}
	}
	out.Logf(slog.LevelInfo, "looked up %d %s: %d found, %d not found", read, plural(read, "record", "records"), found, missing)
	return ctx.Err()
}

// load reads the lookup data into a table by key.
func (t *streamLookup) load(ctx context.Context, info <-chan transform.Batch, from string, out *transform.Output) (map[string]*lookupEntry, int, error) {
	out.Logf(slog.LevelDebug, "waiting for the lookup data from %s (info link)", from)
	most := defaultMaxLookupRecords
	if t.MaxRecords != nil {
		most = *t.MaxRecords
	}
	table := map[string]*lookupEntry{}
	n := 0
	for b := range info {
		for _, r := range b {
			if n++; n > most {
				return nil, n, fmt.Errorf("the lookup data from %s has more than %d records (maxRecords); for larger tables use db.lookup", from, most)
			}
			id, ok := t.key(r, func(k streamLookupKey) string { return k.LookupField })
			if !ok {
				continue // an empty key is never looked up
			}
			if e := table[id]; e != nil {
				e.multiple = true
				continue
			}
			values := make([]any, len(t.Return))
			for i, rt := range t.Return {
				values[i], _ = r.Get(rt.LookupField)
			}
			table[id] = &lookupEntry{values: values}
		}
		if err := ctx.Err(); err != nil {
			return nil, n, err
		}
	}
	return table, n, ctx.Err()
}

// key returns the values of the key fields of a record as text; false if one is empty.
func (t *streamLookup) key(r record.Record, name func(streamLookupKey) string) (string, bool) {
	var b strings.Builder
	for _, k := range t.Keys {
		v, _ := r.Get(name(k))
		if v == nil {
			return "", false
		}
		b.WriteString(expr.Format(v))
		b.WriteByte(0)
	}
	return b.String(), true
}

// describe names the key values of a record for messages, e.g. (customerId = 7).
func (t *streamLookup) describe(r record.Record) string {
	parts := make([]string, len(t.Keys))
	for i, k := range t.Keys {
		v, _ := r.Get(k.Field)
		parts[i] = fmt.Sprintf("%s = %v", k.Field, v)
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func plural(n int, one, more string) string {
	if n == 1 {
		return one
	}
	return more
}
