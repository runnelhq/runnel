package record

import (
	"slices"
	"testing"
)

func TestRecordKeepsOrder(t *testing.T) {
	r := New(Field{"id", 1}, Field{"name", "Ada"}, Field{"id", 2})
	if !slices.Equal(r.Names(), []string{"id", "name"}) {
		t.Fatalf("names %v", r.Names())
	}
	if v, ok := r.Get("id"); !ok || v != 2 {
		t.Errorf("id = %v %v", v, ok)
	}
	r.Set("city", "Kiel")
	r.Set("name", "Grace")
	if !slices.Equal(r.Names(), []string{"id", "name", "city"}) || r.Map()["name"] != "Grace" {
		t.Errorf("after set: %v %v", r.Names(), r.Map())
	}
	r.Delete("name")
	r.Delete("missing")
	if !slices.Equal(r.Names(), []string{"id", "city"}) || r.Len() != 2 {
		t.Errorf("after delete: %v", r.Names())
	}
	if _, ok := r.Get("name"); ok {
		t.Error("deleted field still there")
	}
}

func TestCloneIsIndependent(t *testing.T) {
	a := New(Field{"a", 1}, Field{"b", 2})
	b := a.Clone()
	b.Set("a", 9)
	b.Delete("b")
	b.Set("c", 3)
	if v, _ := a.Get("a"); v != 1 || !slices.Equal(a.Names(), []string{"a", "b"}) {
		t.Errorf("original changed: %v", a.Map())
	}
	// Delete must not move the fields of a record that shares the array.
	c := New(Field{"x", 1}, Field{"y", 2}, Field{"z", 3})
	d := c.Clone()
	d.Delete("x")
	if !slices.Equal(c.Names(), []string{"x", "y", "z"}) {
		t.Errorf("clone delete changed original: %v", c.Names())
	}
	var empty Record
	if empty.Clone().Len() != 0 {
		t.Error("empty clone")
	}
}

func TestMakeFromFieldsGetAt(t *testing.T) {
	r := Make(2)
	r.Set("a", 1)
	r.Set("b", 2)
	if r.Len() != 2 || cap(r.fields) != 2 {
		t.Errorf("make: %+v cap %d", r, cap(r.fields))
	}
	f := FromFields([]Field{{Name: "x", Value: 1}, {Name: "y", Value: 2}})
	if !slices.Equal(f.Names(), []string{"x", "y"}) {
		t.Errorf("from fields %v", f.Names())
	}
	// GetAt looks at the position first and falls back to the name.
	for _, c := range []struct {
		i    int
		name string
		want any
		ok   bool
	}{{0, "x", 1, true}, {1, "y", 2, true}, {0, "y", 2, true}, {5, "x", 1, true}, {-1, "y", 2, true}, {1, "z", nil, false}} {
		if v, ok := f.GetAt(c.i, c.name); v != c.want || ok != c.ok {
			t.Errorf("GetAt(%d, %q) = %v, %v", c.i, c.name, v, ok)
		}
	}
}
