package core

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// core.dedupe passes on the first record of each combination of values and drops the
// records that repeat it. It keeps the combinations seen in memory, so memory grows with
// the number of different records (not with their size); the records themselves go on
// at once.
var dedupeDefinition = transform.Definition{
	Type:        "core.dedupe",
	Category:    transform.CategoryCombine,
	Label:       "Remove Duplicates",
	Description: "Passes on the first record of each combination of values and drops repeated ones",
	Example:     "fields: [email]\nignoreCase: true\n",
	Config:      dedupe{},
	New:         newDedupe,
}

type dedupe struct {
	Fields     []string `json:"fields" doc:"Fields whose values together make a record a duplicate of an earlier one; empty: all fields" schema:"format=runnel-field"`
	IgnoreCase bool     `json:"ignoreCase" doc:"Compare text regardless of upper and lower case" schema:"default=false"`
}

func newDedupe(with map[string]any) (transform.Transform, error) {
	t := &dedupe{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	for i, f := range t.Fields {
		if f == "" {
			return nil, fmt.Errorf("fields[%d]: empty field name", i)
		}
	}
	return t, nil
}

// Init checks the fields and passes the schema on: records are only dropped.
func (t *dedupe) Init(ic *transform.InitContext) (record.Schema, error) {
	for i, f := range t.Fields {
		ic.Use(f, fmt.Sprintf("fields[%d]", i))
	}
	return ic.In, nil
}

func (t *dedupe) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	seen := map[string]struct{}{}
	var read, kept int64
	var key strings.Builder
	for b := range in {
		for _, r := range b {
			read++
			key.Reset()
			if len(t.Fields) > 0 {
				for _, f := range t.Fields {
					v, _ := r.Get(f)
					t.writeKey(&key, v)
				}
			} else {
				// All fields, regardless of their order.
				fields := slices.SortedFunc(slices.Values(r.Fields()), func(a, b record.Field) int { return strings.Compare(a.Name, b.Name) })
				for _, f := range fields {
					key.WriteString(f.Name)
					key.WriteByte(0)
					t.writeKey(&key, f.Value)
				}
			}
			k := key.String()
			if _, dup := seen[k]; dup {
				continue
			}
			seen[k] = struct{}{}
			kept++
			if err := out.Emit(r); err != nil {
				return err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	removed := read - kept
	noun := "duplicates"
	if removed == 1 {
		noun = "duplicate"
	}
	out.Logf(slog.LevelInfo, "kept %d of %d records, removed %d %s", kept, read, removed, noun)
	return nil
}

// writeKey adds a value to the key of a record: equal values give equal keys, also
// across number types (1 = 1.0); the values are separated by a byte no key contains
// unescaped.
func (t *dedupe) writeKey(b *strings.Builder, v any) {
	if s, ok := v.(string); ok && t.IgnoreCase {
		v = strings.ToLower(s)
	}
	k := record.Key(v)
	b.WriteString(strings.ReplaceAll(k, "\x00", "\x00\x01"))
	b.WriteString("\x00\x00")
}
