package model

import (
	"strings"
	"testing"
)

const pipelineSource = `# Customers – comments must survive edits.
kind: Pipeline
apiVersion: runnel/v1
name: customers
transforms:
  - id: read
    type: csv.input
    with:
      path: in.csv   # input
  - id: types
    type: core.convert
  - { id: write, type: csv.output, with: { path: out.csv } }
links:
  - { from: read, to: types }
  - { from: types, to: write }
`

func editPipeline(t *testing.T, src string, e PipelineEdit) string {
	t.Helper()
	out, err := EditPipeline("x.pipeline.yaml", []byte(src), e)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePipeline("x.pipeline.yaml", out); err != nil {
		t.Fatalf("result does not parse: %v\n%s", err, out)
	}
	return string(out)
}

func TestEditPipelineAddMoveAndLinks(t *testing.T) {
	got := editPipeline(t, pipelineSource, PipelineEdit{
		Add:      []NewAction{{ID: "rejected", Type: "csv.output"}},
		Move:     map[string]Point{"rejected": {X: 256, Y: 380}, "read": {X: 16, Y: 130}},
		AddLinks: []PipelineLink{{From: "types", To: "rejected", Type: ErrorLink}},
	})
	want := strings.Replace(pipelineSource, "  - { id: write, type: csv.output, with: { path: out.csv } }\n",
		"  - { id: write, type: csv.output, with: { path: out.csv } }\n  - id: rejected\n    type: csv.output\n", 1)
	want += "  - { from: types, to: rejected, type: error }\nlayout:\n  read: { x: 16, y: 130 }\n  rejected: { x: 256, y: 380 }\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditPipelineLinkType(t *testing.T) {
	got := editPipeline(t, pipelineSource, PipelineEdit{UpdateLinks: []PipelineLinkUpdate{{Index: 1, Type: ptr(ErrorLink)}}})
	if !strings.Contains(got, "  - { from: types, to: write, type: error }\n") {
		t.Errorf("to error:\n%s", got)
	}
	// data is the default and removes the field.
	got = editPipeline(t, got, PipelineEdit{UpdateLinks: []PipelineLinkUpdate{{Index: 1, Type: ptr(DataLink)}}})
	if got != pipelineSource {
		t.Errorf("back to data:\n%s", got)
	}
}

func TestEditPipelineSetAndRename(t *testing.T) {
	got := editPipeline(t, pipelineSource, PipelineEdit{Set: []SetField{
		{ID: "types", Field: "copies", Value: 2}, {ID: "read", Field: "with.delimiter", Value: ";"}}})
	for _, want := range []string{"    type: core.convert\n    copies: 2\n", "      path: in.csv   # input\n      delimiter: ;\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	got = editPipeline(t, pipelineSource, PipelineEdit{SetPipeline: []WorkflowField{{Field: "settings.strict", Value: true}}})
	if !strings.Contains(got, "name: customers\nsettings:\n  strict: true\ntransforms:\n") {
		t.Errorf("settings:\n%s", got)
	}
	got = editPipeline(t, pipelineSource, PipelineEdit{Rename: map[string]string{"types": "convert"}})
	want := strings.NewReplacer("  - id: types\n", "  - id: convert\n", "{ from: read, to: types }", "{ from: read, to: convert }",
		"{ from: types, to: write }", "{ from: convert, to: write }").Replace(pipelineSource)
	if got != want {
		t.Errorf("rename:\n%s", got)
	}
}

func TestEditPipelineDelete(t *testing.T) {
	got := editPipeline(t, pipelineSource, PipelineEdit{Delete: []string{"types"}})
	want := strings.NewReplacer("  - id: types\n    type: core.convert\n", "", "  - { from: read, to: types }\n", "",
		"  - { from: types, to: write }\n", "").Replace(pipelineSource)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditPipelineErrors(t *testing.T) {
	for _, tt := range []struct {
		edit PipelineEdit
		want string
	}{
		{PipelineEdit{Delete: []string{"nope"}}, `cannot delete transform "nope"`},
		{PipelineEdit{Add: []NewAction{{ID: "read", Type: "csv.input"}}}, `cannot add transform "read": the id is already used`},
		{PipelineEdit{AddLinks: []PipelineLink{{From: "read", To: "write", Type: "info"}}}, `link type "info" is not supported here`},
		{PipelineEdit{UpdateLinks: []PipelineLinkUpdate{{Index: 5, Type: ptr(ErrorLink)}}}, "cannot change link #6"},
		{PipelineEdit{Set: []SetField{{ID: "read", Field: "timeout", Value: "1s"}}}, `field "timeout" cannot be changed here`},
		{PipelineEdit{Set: []SetField{{ID: "write", Field: "copies", Value: 2}}}, "flow style"},
		{PipelineEdit{SetPipeline: []WorkflowField{{Field: "settings.timeout", Value: "1h"}}}, `field "settings.timeout" of the pipeline cannot be changed here`},
	} {
		if _, err := EditPipeline("x.pipeline.yaml", []byte(pipelineSource), tt.edit); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%+v: error %v, want containing %q", tt.edit, err, tt.want)
		}
	}
}

func TestPipelineLayout(t *testing.T) {
	p, err := ParsePipeline("x.pipeline.yaml", []byte(pipelineSource+"layout:\n  read: { x: 16, y: 130 }\n  gone: { x: 0, y: 0 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Layout["read"] != (Point{X: 16, Y: 130}) {
		t.Errorf("layout %v", p.Layout)
	}
	if d := p.Validate(); len(d) != 1 || !strings.Contains(d[0].Error(), `layout entry for unknown transform "gone"`) {
		t.Errorf("diags %v", d)
	}
}

// TestEditPipelineSetList checks writing "fields:" from "Detect fields" (S46): one
// mapping per line, texts with , quoted, an existing list replaced, an empty one removed.
func TestEditPipelineSetList(t *testing.T) {
	fields := [][]KeyValue{
		{{Key: "name", Value: "kdnr"}, {Key: "type", Value: "int"}},
		{{Key: "name", Value: "Weber, Anna"}},
		{{Key: "name", Value: "umsatz"}, {Key: "type", Value: "decimal"}, {Key: "format", Value: "#.##0,00"}},
	}
	got := editPipeline(t, pipelineSource, PipelineEdit{Set: []SetField{{ID: "read", Field: "with.fields", List: fields}}})
	want := strings.Replace(pipelineSource, "      path: in.csv   # input\n", `      path: in.csv   # input
      fields:
        - { name: kdnr, type: int }
        - { name: "Weber, Anna" }
        - { name: umsatz, type: decimal, format: "#.##0,00" }
`, 1)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	again := editPipeline(t, got, PipelineEdit{Set: []SetField{{ID: "read", Field: "with.fields", List: fields[:1]}}})
	if !strings.Contains(again, "      fields:\n        - { name: kdnr, type: int }\n  - id: types") || strings.Count(again, "- { name") != 1 {
		t.Errorf("replaced:\n%s", again)
	}
	if removed := editPipeline(t, got, PipelineEdit{Set: []SetField{{ID: "read", Field: "with.fields"}}}); removed != pipelineSource {
		t.Errorf("removed:\n%s", removed)
	}
	if _, err := EditPipeline("x.pipeline.yaml", []byte(pipelineSource), PipelineEdit{Set: []SetField{{ID: "read", Field: "with.fields",
		List: [][]KeyValue{{{Key: "name", Value: "a"}, {Key: "name", Value: "b"}}}}}}); err == nil {
		t.Error("duplicate key: no error")
	}
}
