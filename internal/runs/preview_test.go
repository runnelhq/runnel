package runs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/engine/pipeline"
	"github.com/runnelhq/runnel/internal/transforms"
)

// TestPreview runs examples/pipelines/pipelines/customers.pipeline.yaml up to "types"
// (S44): read and types run, the outputs after them do not; the records of the links of
// types and its output are kept.
func TestPreview(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "examples", "pipelines", "pipelines", "customers.pipeline.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	csv := filepath.Join(dir, "customers.csv")
	if err := os.WriteFile(csv, []byte("kdnr;name;umsatz\n4711;A;1.000,50\n4712;B;k. A.\n4713;C;2,00\n4714;D;0,00\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(data), "path: examples/pipelines/data/customers.csv", "path: "+filepath.ToSlash(csv), 1)
	// Outputs write into the folder; nothing may be written.
	text = strings.ReplaceAll(text, "${OUTPUT}", filepath.ToSlash(filepath.Join(dir, "out.csv")))
	text = strings.ReplaceAll(text, "${REJECTED}", filepath.ToSlash(filepath.Join(dir, "rejected.csv")))

	p, err := PreparePipeline(PipelineSpec{Name: "customers.pipeline.yaml", Data: []byte(text), Registry: transforms.Builtin(),
		Preview: &PreviewSpec{UpTo: "types", Limit: 3}})
	if err != nil {
		t.Fatal(err)
	}
	res := p.Runner.Run(context.Background())
	if res.Status != pipeline.Succeeded {
		t.Fatalf("%s: %v", res.Status, res.Err)
	}
	// Links of the preview, in the order of the file, and the output.
	links := p.Pipeline.Links
	var got []string
	for i, orig := range p.Preview.Links {
		name := "output"
		if orig >= 0 {
			name = links[orig].From + ">" + links[orig].To
		}
		got = append(got, fmt.Sprintf("%s:%d", name, len(res.Samples[i])))
	}
	if fmt.Sprint(got) != "[read>types:3 types>rejected:1 types>active:2 output:2]" || p.Preview.Output != 3 {
		t.Errorf("links %v, output %d", got, p.Preview.Output)
	}
	if c := res.Counts["read"]; c.Written != 3 {
		t.Errorf("read wrote %d records, want the limit 3", c.Written)
	}
	if _, ok := res.Counts["active"]; !ok {
		t.Error("the target of a link of types does not receive its records")
	}
	if _, ok := res.Counts["map"]; ok {
		t.Error("map runs, but is after types")
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "*.csv")); len(files) != 1 {
		t.Errorf("files written: %v", files)
	}
	if v, _ := res.Samples[p.Preview.Output][0].Get("kdnr"); v != int64(4711) {
		t.Errorf("output: kdnr %#v", v)
	}

	_, err = PreparePipeline(PipelineSpec{Name: "x.pipeline.yaml", Data: []byte(text), Registry: transforms.Builtin(),
		Preview: &PreviewSpec{UpTo: "nope", Limit: 3}})
	if err == nil || !strings.Contains(err.Error(), `no transform "nope"`) {
		t.Errorf("unknown transform: %v", err)
	}
}
