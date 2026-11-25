package runs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/engine/pipeline"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/plugin/plugintest"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transforms"
)

const countPipeline = `kind: Pipeline
apiVersion: runnel/v1
name: count
transforms:
  - id: count
    type: demo.count
    with:
      count: 250
  - id: result
    type: core.to-result
links:
  - { from: count, to: result }
`

const upperPipeline = `kind: Pipeline
apiVersion: runnel/v1
name: upper
transforms:
  - id: in
    type: core.from-result
  - id: up
    type: demo.upper
    with:
      field: name
      to: routed
  - id: ok
    type: core.to-result
  - id: routed
    type: core.dummy
  - id: rejected
    type: core.dummy
links:
  - { from: in, to: up }
  - { from: up, to: ok }
  - { from: up, to: routed }
  - { from: up, to: rejected, type: error }
`

func runPluginPipeline(t *testing.T, dir, text string, rows ...record.Record) pipeline.Result {
	t.Helper()
	p, err := PreparePipeline(PipelineSpec{Name: "pipelines/p.pipeline.yaml", Data: []byte(text), ProjectDir: dir,
		Registry: transforms.Builtin(), Rows: rows})
	if err != nil {
		t.Fatal(err)
	}
	return p.Runner.Run(context.Background())
}

func names(values ...string) []record.Record {
	out := make([]record.Record, len(values))
	for i, v := range values {
		out[i] = record.New(record.Field{Name: "id", Value: int64(i + 1)}, record.Field{Name: "name", Value: v})
	}
	return out
}

// An input transform of a plugin emits records of every value type (S50a); the host
// reads them as the types of the header.
func TestPluginTransformInput(t *testing.T) {
	dir := pluginProject(t)
	trustDemo(t, dir)
	res := runPluginPipeline(t, dir, countPipeline)
	if res.Status != pipeline.Succeeded || len(res.Rows) != 250 {
		t.Fatalf("%s %v: %d rows", res.Status, res.Err, len(res.Rows))
	}
	r := res.Rows[1]
	want := map[string]any{
		"n": int64(2), "day": record.Date{Year: 2026, Month: 11, Day: 24}, "raw": []byte("hi"), "ok": true, "ratio": 0.5,
		"name": "row 2", "any": nil,
	}
	for k, v := range want {
		if got, _ := r.Get(k); !equalValue(got, v) {
			t.Errorf("%s: %#v, want %#v", k, got, v)
		}
	}
	if got, _ := r.Get("amount"); !got.(decimal.Decimal).Equal(decimal.RequireFromString("12.5")) {
		t.Errorf("amount %v", got)
	}
	if got, _ := r.Get("at"); !got.(time.Time).Equal(time.Date(2026, 11, 24, 9, 30, 0, 0, time.UTC)) {
		t.Errorf("at %v", got)
	}
	if got, _ := r.Get("doc"); got.(map[string]any)["k"] != int64(2) {
		t.Errorf("doc %#v", got)
	}
	if c := res.Counts["count"]; c.Written != 250 {
		t.Errorf("counts %+v", c)
	}
}

func equalValue(a, b any) bool {
	if x, ok := a.([]byte); ok {
		y, _ := b.([]byte)
		return string(x) == string(y)
	}
	return a == b
}

// A transform of a plugin gets the records of its input as a stream and emits to its
// data links, to a target of its configuration and to its error link.
func TestPluginTransformStream(t *testing.T) {
	dir := pluginProject(t)
	trustDemo(t, dir)
	in := names("a", "bad", "route", "b")
	for i := range 997 { // more batches than the windows of both sides
		in = append(in, names("x"+strings.Repeat("y", i%3))...)
	}
	res := runPluginPipeline(t, dir, upperPipeline, in...)
	if res.Status != pipeline.Succeeded {
		t.Fatalf("%s %v", res.Status, res.Err)
	}
	if c := res.Counts["up"]; c.Read != 1001 || c.Written != 1000 || c.Errors != 1 {
		t.Errorf("up %+v", c)
	}
	if res.Counts["routed"].Read != 1 || res.Counts["rejected"].Read != 1 || len(res.Rows) != 999 {
		t.Errorf("counts %+v rows %d", res.Counts, len(res.Rows))
	}
	if v, _ := res.Rows[0].Get("name"); v != "A" {
		t.Errorf("first %v", res.Rows[0])
	}
	if v, _ := res.Rows[1].Get("name"); v != "B" {
		t.Errorf("second %v", res.Rows[1])
	}
}

// A plugin may leave out the fields of an emit when they are those of the last emit
// (ADR 0019); the host then reads the rows with those fields.
func TestPluginTransformSameFields(t *testing.T) {
	dir := pluginProject(t)
	t.Setenv(plugintest.ModeEnv, "same-fields")
	trustDemo(t, dir)
	in := names("a", "bad", "route", "b")
	for range 997 {
		in = append(in, names("x")...)
	}
	res := runPluginPipeline(t, dir, upperPipeline, in...)
	if res.Status != pipeline.Succeeded {
		t.Fatalf("%s %v", res.Status, res.Err)
	}
	if c := res.Counts["up"]; c.Read != 1001 || c.Written != 1000 || c.Errors != 1 || len(res.Rows) != 999 {
		t.Errorf("up %+v, rows %d", c, len(res.Rows))
	}
	if v, _ := res.Rows[998].Get("name"); v != "X" {
		t.Errorf("last %v", res.Rows[998])
	}
	res = runPluginPipeline(t, dir, countPipeline)
	if res.Status != pipeline.Succeeded || len(res.Rows) != 250 {
		t.Fatalf("input: %s %v, %d rows", res.Status, res.Err, len(res.Rows))
	}
	if v, _ := res.Rows[249].Get("name"); v != "row 250" {
		t.Errorf("last %v", res.Rows[249])
	}
}

// Errors of the stream stop the pipeline with a message: a stream closed with an error,
// emits to a target outside the configuration, more emits than the window, a crash, a
// plugin that is not trusted.
func TestPluginTransformErrors(t *testing.T) {
	dir := pluginProject(t)
	trustDemo(t, dir)
	for _, tt := range []struct {
		mode, text string
		rows       []record.Record
		want       string
	}{
		{"close-error", upperPipeline, names("a"), `transform "up": cannot reach the server with s3cret`},
		{"bad-to", upperPipeline, names("a"), `transform "up": the plugin emitted records to "nowhere", which is not a target in the configuration`},
		{"normal", upperPipeline, names("a", "crash"), `transform "up": the plugin ended unexpectedly; last lines: panic: crash in upper`},
	} {
		t.Setenv(plugintest.ModeEnv, tt.mode)
		trustDemo(t, dir)
		res := runPluginPipeline(t, dir, tt.text, tt.rows...)
		if res.Status != pipeline.Failed || res.Err == nil || !strings.Contains(res.Err.Error(), tt.want) {
			t.Errorf("%s: %s %v", tt.mode, res.Status, res.Err)
		}
	}
}

// Without approval the pipeline does not start; validate names the reason at the transform.
func TestPluginTransformNotTrusted(t *testing.T) {
	dir := pluginProject(t)
	_, err := PreparePipeline(PipelineSpec{Name: "pipelines/p.pipeline.yaml", Data: []byte(countPipeline), ProjectDir: dir, Registry: transforms.Builtin()})
	want := `transform "count": plugin "demo" is not trusted on this computer; check it and run "runnel plugins trust"`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("run: %v", err)
	}
	pl, err := model.ReadPipeline("p.pipeline.yaml", []byte(countPipeline))
	if err != nil {
		t.Fatal(err)
	}
	project, err := model.LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if diags := CheckPipeline(pl, nil, transforms.Builtin(), project); !strings.Contains(diags.Error(), want) {
		t.Errorf("validate: %v", diags)
	}
}

const benchPipeline = `kind: Pipeline
apiVersion: runnel/v1
name: bench
transforms:
  - id: gen
    type: core.generate-rows
    with:
      rows: 200000
      rowNumberField: id
      fields:
        - { name: name, value: customer }
        - { name: amount, value: 1234.5 }
        - { name: city, value: Berlin }
        - { name: active, value: true }
  - id: up
    type: demo.upper
    with:
      field: city
  - id: out
    type: core.dummy
links:
  - { from: gen, to: up }
  - { from: up, to: out }
`

// BenchmarkPluginTransform passes 200,000 records with five fields through a transform
// of the test plugin and back (ADR 0019: at least 300,000 records/s).
func BenchmarkPluginTransform(b *testing.B) {
	dir := pluginProject(b)
	trustDemo(b, dir)
	b.ReportAllocs()
	var records int64
	for b.Loop() {
		p, err := PreparePipeline(PipelineSpec{Name: "pipelines/p.pipeline.yaml", Data: []byte(benchPipeline), ProjectDir: dir, Registry: transforms.Builtin()})
		if err != nil {
			b.Fatal(err)
		}
		res := p.Runner.Run(context.Background())
		if res.Status != pipeline.Succeeded || res.Counts["out"].Read != 200000 {
			b.Fatalf("%s %v %+v", res.Status, res.Err, res.Counts)
		}
		records += res.Counts["out"].Read
	}
	b.ReportMetric(float64(records)/b.Elapsed().Seconds(), "records/s")
}

const initPipeline = `kind: Pipeline
apiVersion: runnel/v1
name: init
transforms:
  - id: count
    type: demo.count
    with:
      count: 2
      password: ${secret:crm-token}
  - id: up
    type: demo.upper
    with:
      field: name
  - id: out
    type: core.dummy
links:
  - { from: count, to: up }
  - { from: up, to: out }
`

// transform.init gives the schema of the output and problems of the configuration at
// validate, in the editor and before the run (S50b); secrets are sent empty.
func TestPluginTransformInit(t *testing.T) {
	dir := pluginProject(t)
	trustDemo(t, dir)
	p, err := PreparePipeline(PipelineSpec{Name: "pipelines/p.pipeline.yaml", Data: []byte(initPipeline), ProjectDir: dir, Registry: transforms.Builtin()})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Warnings.Error(); !strings.Contains(got, `transform "count": init got {"count":2,"password":""}, targets [up]`) {
		t.Errorf("warnings %s", got)
	}
	project, err := model.LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg := WithPluginTransforms(transforms.Builtin(), dir)
	pl, err := model.ReadPipeline("p.pipeline.yaml", []byte(initPipeline))
	if err != nil {
		t.Fatal(err)
	}
	schemas := pipeline.Schemas(pl, pipeline.Options{Registry: reg})
	if out := schemas["up"].Output; out.Open || len(out.Fields) != 10 || out.Fields[1] != (record.FieldDef{Name: "amount", Type: record.Decimal}) {
		t.Errorf("schema of up %+v", out)
	}
	bad, err := model.ReadPipeline("p.pipeline.yaml", []byte(strings.Replace(initPipeline, "field: name", "field: nope", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if got := CheckPipeline(bad, nil, transforms.Builtin(), project).Error(); !strings.Contains(got, `13:14: transform "up": field "nope" is not in the input`) {
		t.Errorf("validate %s", got)
	}
}
