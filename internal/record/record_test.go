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
