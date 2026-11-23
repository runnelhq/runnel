package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// core.merge-join joins the records of two inputs that are sorted by the same keys, like
// a join in a database (S23c): it reads both in step and holds only the records of one key
// at a time, so the inputs may be of any size. Records with the same key on both sides
// give every combination (n × m).
var mergeJoinDefinition = transform.Definition{
	Type:        "core.merge-join",
	Category:    transform.CategoryCombine,
	Label:       "Merge Join",
	Description: "Joins the records of two inputs sorted by the same keys, like a join in a database",
	Example:     "left: orders\nright: customers\nkeys:\n  - { left: customerId, right: id }\njoin: left\n",
	Config:      mergeJoin{},
	New:         newMergeJoin,
	NamedInputs: true,
}

type mergeJoin struct {
	Left  string     `json:"left" doc:"Transform whose data link brings the left records" schema:"required,format=runnel-source,example=orders"`
	Right string     `json:"right" doc:"Transform whose data link brings the right records" schema:"required,format=runnel-source,example=customers"`
	Keys  []mergeKey `json:"keys" doc:"Fields that must be equal; both inputs must be sorted by them in ascending order, e.g. with core.sort" schema:"required"`
	Join  string     `json:"join" doc:"inner: only records with a partner; left: also left records without one; full: also right records without one" schema:"enum=inner|left|full,default=inner"`

	// Fields of the left and right records known from Init, set empty in records without
	// a partner.
	leftFields, rightFields []string
}

type mergeKey struct {
	Left  string `json:"left" doc:"Field of the left records" schema:"required,format=runnel-field,example=customerId"`
	Right string `json:"right" doc:"Field of the right records; empty: the same name" schema:"format=runnel-field,example=id"`
}

func newMergeJoin(with map[string]any) (transform.Transform, error) {
	t := &mergeJoin{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	if t.Left == t.Right {
		return nil, fmt.Errorf("left and right are both %q; name two different transforms", t.Left)
	}
	if len(t.Keys) == 0 {
		return nil, errors.New("keys: name at least one field to compare")
	}
	for i := range t.Keys {
		if t.Keys[i].Right == "" {
			t.Keys[i].Right = t.Keys[i].Left
		}
	}
	switch t.Join {
	case "", "inner", "left", "full":
	default:
		return nil, fmt.Errorf("invalid join %q (expected inner, left or full)", t.Join)
	}
	return t, nil
}

// Init checks that left and right are the transforms of the two incoming links and that
// the keys exist there; the records out have the left fields and then the right ones that
// the left records do not have.
func (t *mergeJoin) Init(ic *transform.InitContext) (record.Schema, error) {
	names := make([]string, 0, len(ic.Inputs))
	for id := range ic.Inputs {
		names = append(names, id)
	}
	slices.Sort(names)
	l, okL := ic.Inputs[t.Left]
	r, okR := ic.Inputs[t.Right]
	for path, id := range map[string]string{"left": t.Left, "right": t.Right} {
		if _, ok := ic.Inputs[id]; !ok {
			ic.Error(path, fmt.Sprintf("no data link comes from %q (incoming: %s)", id, list(names)))
		}
	}
	for _, id := range names {
		if id != t.Left && id != t.Right {
			ic.Error("", fmt.Sprintf("core.merge-join reads two inputs, left and right; the link from %q is not used", id))
		}
	}
	if !okL || !okR {
		return ic.In, nil
	}
	in := ic.In
	for i, k := range t.Keys {
		ic.In = l
		ic.Use(k.Left, fmt.Sprintf("keys[%d].left", i))
		ic.In = r
		ic.Use(k.Right, fmt.Sprintf("keys[%d].right", i))
	}
	ic.In = in
	out := record.Schema{Fields: slices.Clone(l.Fields), Open: l.Open || r.Open}
	t.leftFields, t.rightFields = l.Names(), nil
	for _, f := range r.Fields {
		if _, ok := out.Field(f.Name); !ok {
			out.Fields = append(out.Fields, f)
			t.rightFields = append(t.rightFields, f.Name)
		}
	}
	return out, nil
}

func list(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// joinSide reads one input in groups of records with the same key and checks that they
// arrive sorted.
type joinSide struct {
	name   string
	ch     <-chan transform.Batch
	fields []string // the key fields
	buf    transform.Batch
	pos    int
	peeked *record.Record
	last   []any // the key of the group before
	read   int
}

func (s *joinSide) next() (record.Record, bool) {
	if s.peeked != nil {
		r := *s.peeked
		s.peeked = nil
		s.read++
		return r, true
	}
	for s.pos >= len(s.buf) {
		b, ok := <-s.ch
		if !ok {
			return record.Record{}, false
		}
		s.buf, s.pos = b, 0
	}
	r := s.buf[s.pos]
	s.pos++
	s.read++
	return r, true
}

func (s *joinSide) key(r record.Record) []any {
	k := make([]any, len(s.fields))
	for i, f := range s.fields {
		k[i], _ = r.Get(f)
	}
	return k
}

// group returns the next records with the same key and the key; none at the end. A key
// with an empty value matches nothing and makes a group of its own.
func (s *joinSide) group() ([]record.Record, []any, error) {
	first, ok := s.next()
	if !ok {
		return nil, nil, nil
	}
	key := s.key(first)
	if s.last != nil {
		c, err := compareKeys(s.last, key, s.fields, s.fields)
		if err != nil {
			return nil, nil, fmt.Errorf("%s, record %d: %w", s.name, s.read, err)
		}
		if c > 0 {
			return nil, nil, fmt.Errorf("the records of %s are not sorted by %s: record %d (%s) comes after %s; sort them before, e.g. with core.sort",
				s.name, strings.Join(s.fields, ", "), s.read, formatKey(key), formatKey(s.last))
		}
	}
	s.last = key
	group := []record.Record{first}
	if hasNull(key) {
		return group, key, nil
	}
	for {
		r, ok := s.next()
		if !ok {
			return group, key, nil
		}
		k := s.key(r)
		c, err := compareKeys(key, k, s.fields, s.fields)
		if err != nil {
			return nil, nil, fmt.Errorf("%s, record %d: %w", s.name, s.read, err)
		}
		if c != 0 || hasNull(k) {
			s.peeked = &r
			s.read--
			return group, key, nil
		}
		group = append(group, r)
	}
}

func hasNull(key []any) bool { return slices.Contains(key, nil) }

func formatKey(key []any) string {
	parts := make([]string, len(key))
	for i, v := range key {
		parts[i] = fmt.Sprint(v)
		if v == nil {
			parts[i] = "empty"
		}
	}
	return strings.Join(parts, ", ")
}

// compareKeys orders two keys like core.sort: by each field, empty values last. Values of
// different kinds (e.g. a number and a text) cannot be compared.
func compareKeys(a, b []any, fa, fb []string) (int, error) {
	for i := range a {
		x, y := a[i], b[i]
		switch {
		case x == nil && y == nil:
			continue
		case x == nil:
			return 1, nil
		case y == nil:
			return -1, nil
		}
		kx, ky := record.KindOf(x), record.KindOf(y)
		if kx != ky || kx == record.Incomparable {
			return 0, fmt.Errorf("%s (%s) and %s (%s) cannot be compared; convert them to the same type, e.g. with core.convert", fa[i], record.TypeOf(x), fb[i], record.TypeOf(y))
		}
		if c := record.Compare(x, y); c != 0 {
			return c, nil
		}
	}
	return 0, nil
}

func (t *mergeJoin) Process(ctx context.Context, _ <-chan transform.Batch, out *transform.Output) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	left, right := out.Input(t.Left), out.Input(t.Right)
	for name, ch := range map[string]<-chan transform.Batch{t.Left: left, t.Right: right} {
		if ch == nil {
			return fmt.Errorf("no data link comes from %q", name)
		}
	}
	lf, rf := make([]string, len(t.Keys)), make([]string, len(t.Keys))
	for i, k := range t.Keys {
		lf[i], rf[i] = k.Left, k.Right
	}
	ls := &joinSide{name: t.Left, ch: left, fields: lf}
	rs := &joinSide{name: t.Right, ch: right, fields: rf}
	lg, lk, err := ls.group()
	if err != nil {
		return err
	}
	rg, rk, err := rs.group()
	if err != nil {
		return err
	}
	var pairs, leftOnly, rightOnly int
	emit := func(r record.Record) error { return out.Emit(r) }
	for lg != nil || rg != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		var c int
		switch {
		case lg == nil:
			c = 1
		case rg == nil:
			c = -1
		default:
			if c, err = compareKeys(lk, rk, lf, rf); err != nil {
				return err
			}
			if c == 0 && hasNull(lk) {
				c = -1 // empty keys match nothing
			}
		}
		switch {
		case c == 0:
			for _, l := range lg {
				for _, r := range rg {
					pairs++
					if err := emit(t.combine(l.Clone(), r)); err != nil {
						return err
					}
				}
			}
		case c < 0 && t.Join != "" && t.Join != "inner":
			for _, l := range lg {
				leftOnly++
				if err := emit(t.combine(l, record.Record{})); err != nil {
					return err
				}
			}
		case c > 0 && t.Join == "full":
			for _, r := range rg {
				rightOnly++
				if err := emit(t.rightAlone(r)); err != nil {
					return err
				}
			}
		}
		if c <= 0 {
			if lg, lk, err = ls.group(); err != nil {
				return err
			}
		}
		if c >= 0 {
			if rg, rk, err = rs.group(); err != nil {
				return err
			}
		}
	}
	out.Logf(slog.LevelInfo, "joined %d left and %d right %s: %d %s, %d left only, %d right only",
		ls.read, rs.read, plural(rs.read, "record", "records"), pairs, plural(pairs, "pair", "pairs"), leftOnly, rightOnly)
	return ctx.Err()
}

// combine adds the fields of the right record that the left one does not have; without a
// right record, the right fields known from Init are empty.
func (t *mergeJoin) combine(l, r record.Record) record.Record {
	if r.Fields() == nil {
		for _, f := range t.rightFields {
			if _, ok := l.Get(f); !ok {
				l.Set(f, nil)
			}
		}
		return l
	}
	for _, f := range r.Fields() {
		if _, ok := l.Get(f.Name); !ok {
			l.Set(f.Name, f.Value)
		}
	}
	return l
}

// rightAlone is a right record without a left one: the left fields are empty except the
// keys, which get the values of the right keys.
func (t *mergeJoin) rightAlone(r record.Record) record.Record {
	l := record.New()
	for _, f := range t.leftFields {
		l.Set(f, nil)
	}
	for _, k := range t.Keys {
		v, _ := r.Get(k.Right)
		l.Set(k.Left, v)
	}
	return t.combine(l, r)
}
