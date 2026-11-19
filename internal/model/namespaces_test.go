package model

import "testing"

func TestTypeNames(t *testing.T) {
	for typ, want := range map[string]bool{
		"core.log":          true,
		"http.request":      true,
		"mcp-github.create": true,
		"sap.rfc-call":      true,
		"a.b.c":             true,
		"log":               false,
		"Core.log":          false,
		"core.":             false,
		".log":              false,
		"core log":          false,
	} {
		if got := ValidTypeName(typ); got != want {
			t.Errorf("ValidTypeName(%q) = %v, want %v", typ, got, want)
		}
	}
	if ns := TypeNamespace("sap.rfc-call"); ns != "sap" {
		t.Errorf("TypeNamespace = %q", ns)
	}
	for ns, want := range map[string]bool{"core": true, "file": true, "ai": true, "mcp": true, "mcp-github": true, "sap": false, "corex": false} {
		if got := IsReservedNamespace(ns); got != want {
			t.Errorf("IsReservedNamespace(%q) = %v, want %v", ns, got, want)
		}
	}
}
