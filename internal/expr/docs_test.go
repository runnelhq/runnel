package expr

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestDocsExamplesCompile(t *testing.T) {
	for _, d := range append(slices.Clone(Roots), Functions...) {
		if d.Description == "" || d.Type == "" {
			t.Errorf("%s: description and type are required", d.Name)
		}
		if d.Example == "" {
			continue
		}
		if _, err := Compile(d.Example, WorkflowRoots); err != nil {
			t.Errorf("%s: example %s: %v", d.Name, d.Example, err)
		}
		if d.Signature != "" && !strings.HasPrefix(d.Signature, d.Name+"(") {
			t.Errorf("%s: signature %q does not start with the name", d.Name, d.Signature)
		}
	}
}

// Every name of WorkflowRoots and every function of the library has a description.
func TestDocsComplete(t *testing.T) {
	var roots []string
	for _, r := range Roots {
		roots = append(roots, r.Name)
	}
	if !slices.Equal(slices.Sorted(slices.Values(roots)), slices.Sorted(slices.Values(WorkflowRoots))) {
		t.Errorf("roots %v, want %v", roots, WorkflowRoots)
	}
	src, err := os.ReadFile("functions.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range regexp.MustCompile(`(?:Function|dateFunc)\("(\w+)"`).FindAllStringSubmatch(string(src), -1) {
		if !slices.ContainsFunc(Functions, func(d Doc) bool { return d.Name == m[1] }) {
			t.Errorf("function %s has no description in Functions", m[1])
		}
	}
}
