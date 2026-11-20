package pipeline

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/core"
)

// collected holds the records of test.collect transforms by id.
type collected struct {
	mu   sync.Mutex
	recs map[string][]record.Record
}

// testRegistry has the core transforms and test transforms:
//
//	test.collect  stores the records it receives (by transform id) and passes them on
//	test.fail     fails after "after" records
//	test.panic    panics on the first record
//	test.first    reads one record, passes it on and stops reading
//	test.change   sets field "changed" on every record (to check that copies are independent)
func testRegistry(c *collected) *transform.Registry {
	r := transform.NewRegistry()
	core.Register(r)
	reg := func(typ string, f func(with map[string]any, ctx context.Context, in <-chan transform.Batch, out *transform.Output) error) {
		r.Register(transform.Definition{Type: typ, Category: transform.CategoryFlow, Label: "Test", Description: "Test",
			New: func(with map[string]any) (transform.Transform, error) {
				return fn(func(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
					return f(with, ctx, in, out)
				}), nil
			}})
	}
	r.Register(transform.Definition{Type: "test.open", Category: transform.CategoryFlow, Label: "Test", Description: "Test",
		New: func(map[string]any) (transform.Transform, error) {
			return openSchema{fn(func(_ context.Context, in <-chan transform.Batch, out *transform.Output) error {
				for b := range in {
					for _, rec := range b {
						if err := out.Emit(rec); err != nil {
							return err
						}
					}
				}
				return nil
			})}, nil
		}})
	// test.odd passes even values of n on and sends odd ones to its error links.
	r.Register(transform.Definition{Type: "test.odd", Category: transform.CategoryFlow, Label: "Test", Description: "Test", RecordErrors: true,
		New: func(map[string]any) (transform.Transform, error) {
			return fn(func(_ context.Context, in <-chan transform.Batch, out *transform.Output) error {
				for b := range in {
					for _, rec := range b {
						n, _ := rec.Get("n")
						var err error
						if n.(int64)%2 == 1 {
							err = out.Fail(rec, "n", fmt.Errorf("odd number %d", n))
						} else {
							err = out.Emit(rec)
						}
						if err != nil {
							return err
						}
					}
				}
				return nil
			}), nil
		}})
	reg("test.collect", func(_ map[string]any, _ context.Context, in <-chan transform.Batch, out *transform.Output) error {
		for b := range in {
			for _, rec := range b {
				c.mu.Lock()
				c.recs[out.TransformID()] = append(c.recs[out.TransformID()], rec)
				c.mu.Unlock()
				if err := out.Emit(rec); err != nil {
					return err
				}
			}
		}
		return nil
	})
	reg("test.fail", func(with map[string]any, _ context.Context, in <-chan transform.Batch, _ *transform.Output) error {
		n := 0
		for b := range in {
			n += len(b)
			if after, _ := strconv.Atoi(fmt.Sprint(with["after"])); n >= after {
				return errors.New("broken record")
			}
		}
		return nil
	})
	reg("test.panic", func(_ map[string]any, _ context.Context, in <-chan transform.Batch, _ *transform.Output) error {
		for range in {
			panic("boom")
		}
		return nil
	})
	reg("test.first", func(_ map[string]any, _ context.Context, in <-chan transform.Batch, out *transform.Output) error {
		b := <-in
		return out.Emit(b[0])
	})
	reg("test.change", func(_ map[string]any, _ context.Context, in <-chan transform.Batch, out *transform.Output) error {
		for b := range in {
			for _, rec := range b {
				rec.Set("changed", true)
				if err := out.Emit(rec); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return r
}

type fn func(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error

// openSchema passes records on and reports an open schema with the incoming fields.
type openSchema struct{ fn }

func (openSchema) Init(ic *transform.InitContext) (record.Schema, error) {
	s := ic.In
	s.Open = true
	return s, nil
}

func (f fn) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	return f(ctx, in, out)
}

func parse(t *testing.T, body string) *model.Pipeline {
	t.Helper()
	p, err := model.ParsePipeline("test.pipeline.yaml", []byte("kind: Pipeline\napiVersion: runnel/v1\nname: test\n"+body))
	if err != nil {
		t.Fatal(err)
	}
	if diags := p.Validate(); diags.HasErrors() {
		t.Fatal(diags)
	}
	return p
}

// run prepares and runs a pipeline with small batches, so that records cross several batches.
func run(ctx context.Context, t *testing.T, body string, with func(*Options)) (Result, *collected, []Event) {
	t.Helper()
	c := &collected{recs: map[string][]record.Record{}}
	var mu sync.Mutex
	var events []Event
	opts := Options{Registry: testRegistry(c), BatchSize: 7, BufferSize: 21, Observer: func(e Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}}
	if with != nil {
		with(&opts)
	}
	r, err := Prepare(parse(t, body), opts)
	if err != nil {
		t.Fatal(err)
	}
	return r.Run(ctx), c, events
}

func finished(events []Event, id string) Event {
	for _, e := range events {
		if e.Kind == TransformFinished && e.TransformID == id {
			return e
		}
	}
	return Event{}
}

func TestRunChainCountsRecords(t *testing.T) {
	res, c, events := run(context.Background(), t, `
transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 100, rowNumberField: n } }
  - { id: pass, type: core.dummy }
  - { id: sink, type: test.collect }
links:
  - { from: gen, to: pass }
  - { from: pass, to: sink }
`, nil)
	if res.Status != Succeeded || res.Err != nil {
		t.Fatalf("%s %v", res.Status, res.Err)
	}
	if got := res.Counts; got["gen"] != (Counts{Read: 0, Written: 100}) || got["pass"] != (Counts{Read: 100, Written: 100}) || got["sink"] != (Counts{Read: 100, Written: 100}) {
		t.Errorf("counts %+v", got)
	}
	// Order within one link is kept.
	recs := c.recs["sink"]
	for i, rec := range recs {
		if v, _ := rec.Get("n"); v != int64(i+1) {
			t.Fatalf("record %d has n=%v", i, v)
		}
	}
	if e := finished(events, "pass"); e.Status != Succeeded || e.Counts != (Counts{Read: 100, Written: 100}) {
		t.Errorf("event %+v", e)
	}
	if events[0].Kind != RunStarted || events[len(events)-1].Kind != RunFinished || len(res.RunID) != 26 {
		t.Errorf("first/last events %v %v, run id %q", events[0].Kind, events[len(events)-1].Kind, res.RunID)
	}
}

func TestRunCopiesToAllOutputsAndMergesInputs(t *testing.T) {
	res, c, _ := run(context.Background(), t, `
transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 50 } }
  - { id: a, type: test.change }
  - { id: b, type: test.collect }
  - { id: both, type: test.collect }
links:
  - { from: gen, to: a }
  - { from: gen, to: b }
  - { from: a, to: both }
  - { from: b, to: both }
`, nil)
	if res.Status != Succeeded {
		t.Fatalf("%s %v", res.Status, res.Err)
	}
	// Every output gets every record (copy); the change in "a" does not reach "b".
	if len(c.recs["b"]) != 50 || len(c.recs["both"]) != 100 {
		t.Errorf("b %d, both %d", len(c.recs["b"]), len(c.recs["both"]))
	}
	for _, rec := range c.recs["b"] {
		if _, changed := rec.Get("changed"); changed {
			t.Fatal("a record was changed by another branch")
		}
	}
	if res.Counts["gen"].Written != 50 || res.Counts["both"].Read != 100 {
		t.Errorf("counts %+v", res.Counts)
	}
}

func TestRunFailureStopsAllTransforms(t *testing.T) {
	res, _, events := run(context.Background(), t, `
transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 100000000 } }
  - { id: bad, type: test.fail, with: { after: 30 } }
links:
  - { from: gen, to: bad }
`, nil)
	if res.Status != Failed || res.Err == nil || res.Err.Error() != `transform "bad": broken record` {
		t.Fatalf("%s %v", res.Status, res.Err)
	}
	// The source stopped long before its 100 million records.
	if e := finished(events, "gen"); e.Status != Cancelled || e.Counts.Written >= 100000000 {
		t.Errorf("gen %+v", e)
	}
	if e := finished(events, "bad"); e.Status != Failed || e.Err == nil {
		t.Errorf("bad %+v", e)
	}
}

func TestRunPanicBecomesError(t *testing.T) {
	res, _, _ := run(context.Background(), t, `
transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 10 } }
  - { id: p, type: test.panic }
links:
  - { from: gen, to: p }
`, nil)
	if res.Status != Failed || !strings.Contains(fmt.Sprint(res.Err), `transform "p": internal error: boom`) {
		t.Errorf("%s %v", res.Status, res.Err)
	}
}

func TestRunTransformThatStopsReadingDoesNotBlock(t *testing.T) {
	done := make(chan Result, 1)
	go func() {
		res, _, _ := run(context.Background(), t, `
transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 10000 } }
  - { id: first, type: test.first }
  - { id: sink, type: test.collect }
links:
  - { from: gen, to: first }
  - { from: first, to: sink }
`, nil)
		done <- res
	}()
	select {
	case res := <-done:
		if res.Status != Succeeded || res.Counts["sink"].Read != 1 || res.Counts["gen"].Written != 10000 {
			t.Errorf("%s %v %+v", res.Status, res.Err, res.Counts)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("pipeline blocked")
	}
}

func TestRunCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	res, _, _ := run(ctx, t, `
transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 1000000000 } }
  - { id: pass, type: core.dummy }
links:
  - { from: gen, to: pass }
`, nil)
	if res.Status != Cancelled || res.Err == nil {
		t.Errorf("%s %v", res.Status, res.Err)
	}
}

func TestPrepareErrors(t *testing.T) {
	c := &collected{recs: map[string][]record.Record{}}
	p := parse(t, `
parameters:
  - { name: N, default: "x" }
transforms:
  - { id: gen, type: core.generate-rows, with: { rows: "${MISSING}" } }
  - { id: what, type: core.nothing }
  - { id: log, type: core.log, with: { level: loud } }
`)
	scope := model.PipelineScope(p, nil, nil, nil)
	diags := Check(p, Options{Registry: testRegistry(c), Scope: scope})
	got := diags.Error()
	for _, want := range []string{
		`transform "gen" (core.generate-rows): rows: variable "MISSING" is undefined`,
		`transform "what": unknown transform type "core.nothing"`,
		`transform "log" (core.log): invalid level "loud"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// TestRunManyRowsWithConstantMemory runs 10 million generated records (1 million with the
// race detector) through the engine and checks that the heap stays small (roadmap S13).
func TestRunManyRowsWithConstantMemory(t *testing.T) {
	rows := manyRows
	if testing.Short() {
		rows = 100000
	}
	c := &collected{recs: map[string][]record.Record{}}
	p := parse(t, fmt.Sprintf(`
transforms:
  - { id: gen, type: core.generate-rows, with: { rows: %d, rowNumberField: n, fields: [ { name: text, value: "some text of a record" } ] } }
  - { id: pass, type: core.dummy }
  - { id: log, type: core.log, with: { every: 1000000, level: debug } }
links:
  - { from: gen, to: pass }
  - { from: pass, to: log }
`, rows))
	r, err := Prepare(p, Options{Registry: testRegistry(c)})
	if err != nil {
		t.Fatal(err)
	}
	var peak uint64
	stop := make(chan struct{})
	sampled := make(chan struct{})
	go func() {
		defer close(sampled)
		var m runtime.MemStats
		for {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
				runtime.ReadMemStats(&m)
				peak = max(peak, m.HeapInuse)
			}
		}
	}()
	res := r.Run(context.Background())
	close(stop)
	<-sampled
	if res.Status != Succeeded || res.Counts["log"].Read != int64(rows) {
		t.Fatalf("%s %v %+v", res.Status, res.Err, res.Counts)
	}
	const limit = 64 << 20
	if peak > limit {
		t.Errorf("heap in use reached %d MiB for %d records, want below %d MiB", peak>>20, rows, limit>>20)
	}
	t.Logf("%d records in %s, peak heap %d MiB", rows, res.Duration, peak>>20)
}

func TestPrepareDerivesSchemas(t *testing.T) {
	c := &collected{recs: map[string][]record.Record{}}
	prepare := func(body string) (*Runner, error) {
		return Prepare(parse(t, body), Options{Registry: testRegistry(c)})
	}
	// Closed schema from generate-rows, passed on by dummy: a typo is an error.
	_, err := prepare(`
transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 1, rowNumberField: n, fields: [ { name: status, value: new } ] } }
  - { id: pass, type: core.dummy }
  - { id: show, type: core.log, with: { fields: [n, stauts] } }
links:
  - { from: gen, to: pass }
  - { from: pass, to: show }
`)
	want := `test.pipeline.yaml:8:53: transform "show": field "stauts" does not exist in the incoming records (did you mean "status"?) (fields: n, status)`
	if err == nil || err.Error() != want {
		t.Errorf("closed:\n got %v\nwant %s", err, want)
	}

	// test.open reports an open schema with the known fields: an unknown field is only a warning.
	r, err := prepare(`
transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 1, rowNumberField: n } }
  - { id: open, type: test.open }
  - { id: show, type: core.log, with: { fields: [n, nn] } }
links:
  - { from: gen, to: open }
  - { from: open, to: show }
`)
	if err != nil {
		t.Fatal(err)
	}
	if w := r.Warnings(); len(w) != 1 || !strings.Contains(w[0].Error(), `warning: transform "show": field "nn" is not known in the incoming records (did you mean "n"?)`) {
		t.Errorf("warnings %v", w)
	}
	// After a transform without Init nothing is known: no warning at all.
	r, err = prepare(`
transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 1 } }
  - { id: other, type: test.collect }
  - { id: show, type: core.log, with: { fields: [anything] } }
links:
  - { from: gen, to: other }
  - { from: other, to: show }
`)
	if err != nil || len(r.Warnings()) != 0 {
		t.Errorf("unknown schema: %v %v", err, r.Warnings())
	}
	if s, _ := r.Schema("other"); !s.Open || len(s.Fields) != 0 {
		t.Errorf("schema without Init: %+v", s)
	}

	// Two inputs: the schemas are merged.
	r, err = prepare(`
transforms:
  - { id: a, type: core.generate-rows, with: { rows: 1, rowNumberField: n, fields: [ { name: x, value: 1 } ] } }
  - { id: b, type: core.generate-rows, with: { rows: 1, rowNumberField: n, fields: [ { name: x, value: text } ] } }
  - { id: both, type: core.dummy }
links:
  - { from: a, to: both }
  - { from: b, to: both }
`)
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := r.Schema("both"); len(s.Fields) != 2 || s.Fields[0].Type != record.Int || s.Fields[1].Type != record.Unknown || s.Open {
		t.Errorf("merged %+v", s)
	}
	if diags := Check(parse(t, "transforms:\n  - { id: a, type: core.log, with: { fields: [x] } }\n"), Options{Registry: testRegistry(c)}); len(diags) != 1 || !diags.HasErrors() {
		t.Errorf("check: %v", diags)
	}
}

// Expressions per record see vars, params (with their type), env and run (ADR 0006).
func TestRunPassesValuesToExpressions(t *testing.T) {
	body := `parameters:
  - { name: MIN, type: integer, default: "4" }
transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 10, rowNumberField: n } }
  - id: keep
    type: core.filter
    with: { condition: "{{ record.n > params.MIN && run.pipeline == 'test' && run.id == 'R1' && vars.MIN == '6' }}" }
  - { id: out, type: test.collect }
links:
  - { from: gen, to: keep }
  - { from: keep, to: out }
`
	res, c, _ := run(context.Background(), t, body, func(o *Options) {
		o.RunID = "R1"
		o.Scope = model.PipelineScope(parse(t, body), nil, nil, map[string]string{"MIN": "6"})
	})
	if res.Status != Succeeded {
		t.Fatalf("%s: %v", res.Status, res.Err)
	}
	if got := len(c.recs["out"]); got != 4 {
		t.Errorf("%d records passed the filter, want 4 (7 to 10)", got)
	}
}

func TestRunErrorLinks(t *testing.T) {
	body := `transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 20, rowNumberField: n } }
  - { id: check, type: test.odd }
  - { id: good, type: test.collect }
  - { id: bad, type: test.collect }
  - { id: bad2, type: test.collect }
links:
  - { from: gen, to: check }
  - { from: check, to: good }
  - { from: check, to: bad, type: error }
  - { from: check, to: bad2, type: error }
`
	res, c, events := run(context.Background(), t, body, nil)
	if res.Status != Succeeded {
		t.Fatalf("%s: %v", res.Status, res.Err)
	}
	if got := res.Counts["check"]; got != (Counts{Read: 20, Written: 10, Errors: 10}) {
		t.Errorf("counts %+v", got)
	}
	if e := finished(events, "check"); e.Counts.Errors != 10 {
		t.Errorf("event counts %+v", e.Counts)
	}
	if len(c.recs["good"]) != 10 || len(c.recs["bad"]) != 10 || len(c.recs["bad2"]) != 10 {
		t.Fatalf("good %d, bad %d, bad2 %d", len(c.recs["good"]), len(c.recs["bad"]), len(c.recs["bad2"]))
	}
	r := c.recs["bad"][0]
	for name, want := range map[string]any{"n": int64(1), "_error": "odd number 1", "_errorField": "n", "_errorTransform": "check"} {
		if v, _ := r.Get(name); v != want {
			t.Errorf("%s = %#v, want %#v", name, v, want)
		}
	}
	// The second error link gets copies.
	c.recs["bad2"][0].Set("n", int64(99))
	if v, _ := r.Get("n"); v != int64(1) {
		t.Error("error links share records")
	}

	// Without an error link, a record error stops the pipeline.
	res, _, _ = run(context.Background(), t, "transforms:\n  - { id: gen, type: core.generate-rows, with: { rows: 3, rowNumberField: n } }\n  - { id: check, type: test.odd }\nlinks:\n  - { from: gen, to: check }\n", nil)
	if res.Status != Failed || res.Err.Error() != `transform "check": odd number 1` {
		t.Errorf("without error link: %s %v", res.Status, res.Err)
	}
}

func TestPrepareErrorLinks(t *testing.T) {
	c := &collected{recs: map[string][]record.Record{}}
	p := parse(t, `transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 1, rowNumberField: n } }
  - { id: pass, type: core.dummy }
  - { id: check, type: test.odd }
  - { id: log, type: core.log, with: { fields: [n, _error, nn] } }
links:
  - { from: gen, to: pass }
  - { from: pass, to: check }
  - { from: pass, to: log, type: error }
  - { from: check, to: log, type: error }
`)
	var msgs []string
	for _, d := range Check(p, Options{Registry: testRegistry(c)}) {
		msgs = append(msgs, d.Error())
	}
	got := strings.Join(msgs, "\n")
	// core.dummy has no record errors. The schema of an error link is the incoming one
	// with the error fields: _error is known, nn is not.
	for _, want := range []string{
		`link #3 (pass → log): core.dummy does not pass records with errors on; the error link is never used`,
		`transform "log": field "nn" does not exist in the incoming records (did you mean "n"?) (fields: n, _error, _errorField, _errorTransform)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if len(msgs) != 2 {
		t.Errorf("%d problems:\n%s", len(msgs), got)
	}
}

func TestRunSwitchRoutesToSingleLinks(t *testing.T) {
	body := `transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 10, rowNumberField: n } }
  - id: route
    type: core.switch
    with:
      cases:
        - { when: "{{ record.n <= 3 }}", to: small }
        - { when: "{{ record.n % 2 == 0 }}", to: even }
      default: rest
  - { id: small, type: test.collect }
  - { id: even, type: test.collect }
  - { id: rest, type: test.collect }
links:
  - { from: gen, to: route }
  - { from: route, to: small }
  - { from: route, to: even }
  - { from: route, to: rest }
`
	res, c, _ := run(context.Background(), t, body, nil)
	if res.Status != Succeeded {
		t.Fatalf("%s: %v", res.Status, res.Err)
	}
	got := fmt.Sprint(len(c.recs["small"]), len(c.recs["even"]), len(c.recs["rest"]))
	if got != "3 4 3" { // 1-3; 4, 6, 8, 10; 5, 7, 9
		t.Errorf("small, even, rest = %s, want 3 4 3", got)
	}
	if got := res.Counts["route"]; got != (Counts{Read: 10, Written: 10}) {
		t.Errorf("counts %+v", got)
	}
	if got := res.Counts["even"]; got.Read != 4 {
		t.Errorf("even read %d", got.Read)
	}

	// A target without a link is found before the run, with its position.
	p := parse(t, strings.Replace(body, "default: rest", "default: rst", 1))
	diags := Check(p, Options{Registry: testRegistry(c)})
	got = diags.Error()
	if len(diags) != 2 || !diags.HasErrors() ||
		!strings.Contains(got, `test.pipeline.yaml:12:16: transform "route": no data link leads to "rst" (did you mean "rest"?) (links to: small, even, rest)`) ||
		!strings.Contains(got, `test.pipeline.yaml:9:7: warning: transform "route": the link to "rest" gets no records`) {
		t.Errorf("diags:\n%s", got)
	}
}

func TestRunCopies(t *testing.T) {
	body := `transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 1000, rowNumberField: n } }
  - id: even
    type: core.filter
    copies: 4
    with: { condition: "{{ record.n % 2 == 0 }}" }
  - { id: out, type: test.collect }
links:
  - { from: gen, to: even }
  - { from: even, to: out }
`
	res, c, events := run(context.Background(), t, body, nil)
	if res.Status != Succeeded {
		t.Fatalf("%s: %v", res.Status, res.Err)
	}
	// The copies share the records: each is filtered once, in any order.
	seen := map[int64]bool{}
	for _, rec := range c.recs["out"] {
		n, _ := rec.Get("n")
		if seen[n.(int64)] || n.(int64)%2 != 0 {
			t.Fatalf("record %d twice or not even", n)
		}
		seen[n.(int64)] = true
	}
	if len(seen) != 500 || res.Counts["even"] != (Counts{Read: 1000, Written: 500}) {
		t.Errorf("%d records, counts %+v", len(seen), res.Counts["even"])
	}
	// One event per transform, whatever the number of copies; each copy logs on its own.
	started, logs := 0, 0
	for _, e := range events {
		if e.TransformID == "even" && e.Kind == TransformStarted {
			started++
		}
		if e.TransformID == "even" && e.Kind == Log {
			logs++
		}
	}
	if started != 1 || logs != 4 {
		t.Errorf("started %d, logs %d; want 1 and 4", started, logs)
	}
}

func TestRunCopyFailureStopsAll(t *testing.T) {
	body := `transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 100, rowNumberField: n, fields: [{ name: s, value: a }] } }
  - id: check
    type: core.filter
    copies: 3
    with: { condition: "{{ record.n > 50 && record.s > 1 }}" }
links:
  - { from: gen, to: check }
`
	res, _, _ := run(context.Background(), t, body, nil)
	if res.Status != Failed || !strings.Contains(res.Err.Error(), `transform "check": record`) || !strings.Contains(res.Err.Error(), "invalid operation: string > int") {
		t.Errorf("%s: %v", res.Status, res.Err)
	}
}

func TestRunDistribute(t *testing.T) {
	body := `transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 9, rowNumberField: n } }
  - { id: split, type: core.dummy, distribution: distribute }
  - { id: a, type: test.collect }
  - { id: b, type: test.collect }
  - { id: c, type: test.collect }
  - { id: errs, type: test.collect }
links:
  - { from: gen, to: split }
  - { from: split, to: a }
  - { from: split, to: b }
  - { from: split, to: c }
`
	res, c, _ := run(context.Background(), t, body, nil)
	if res.Status != Succeeded {
		t.Fatalf("%s: %v", res.Status, res.Err)
	}
	// In turn: a gets 1, 4, 7; b 2, 5, 8; c 3, 6, 9 – each record once.
	for target, want := range map[string]string{"a": "[1 4 7]", "b": "[2 5 8]", "c": "[3 6 9]"} {
		var ns []int64
		for _, rec := range c.recs[target] {
			n, _ := rec.Get("n")
			ns = append(ns, n.(int64))
		}
		if fmt.Sprint(ns) != want {
			t.Errorf("%s: %v, want %s", target, ns, want)
		}
	}
	if res.Counts["split"] != (Counts{Read: 9, Written: 9}) {
		t.Errorf("counts %+v", res.Counts["split"])
	}
}

func TestPrepareSettingsAndCopies(t *testing.T) {
	c := &collected{recs: map[string][]record.Record{}}
	p := parse(t, `settings: { batchSize: 50, bufferSize: 200 }
transforms:
  - { id: gen, type: core.generate-rows, copies: 2, with: { rows: 1 } }
`)
	diags := Check(p, Options{Registry: testRegistry(c)})
	if len(diags) != 1 || !strings.Contains(diags[0].Error(), `test.pipeline.yaml:6:50: transform "gen": core.generate-rows cannot run in several copies`) {
		t.Errorf("copies of an input: %v", diags)
	}
	p = parse(t, "settings: { batchSize: 50, bufferSize: 200 }\ntransforms:\n  - { id: gen, type: core.generate-rows, with: { rows: 1 } }\n")
	r, err := Prepare(p, Options{Registry: testRegistry(c)})
	if err != nil {
		t.Fatal(err)
	}
	if r.opts.BatchSize != 50 || r.opts.BufferSize != 200 {
		t.Errorf("batch %d, buffer %d", r.opts.BatchSize, r.opts.BufferSize)
	}
}

func TestPrepareStrict(t *testing.T) {
	c := &collected{recs: map[string][]record.Record{}}
	body := `settings: { strict: true }
transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 1, rowNumberField: n } }
  - { id: open, type: test.open }
  - { id: log, type: core.log, with: { fields: [n, m] } }
links:
  - { from: gen, to: open }
  - { from: open, to: log }
`
	got := Check(parse(t, body), Options{Registry: testRegistry(c)}).Error()
	for _, want := range []string{
		`test.pipeline.yaml:7:7: transform "open": the fields of its records are not all known, but settings.strict requires it`,
		`transform "log": field "m" does not exist in the incoming records`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// Without strict mode: only a warning for m.
	diags := Check(parse(t, strings.Replace(body, "strict: true", "strict: false", 1)), Options{Registry: testRegistry(c)})
	if len(diags) != 1 || diags.HasErrors() {
		t.Errorf("not strict: %v", diags)
	}
}

// Schemas works despite errors: a transform that cannot be created counts as unknown.
func TestSchemasDespiteErrors(t *testing.T) {
	c := &collected{recs: map[string][]record.Record{}}
	p := parse(t, `
transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 1, rowNumberField: n } }
  - { id: conv, type: core.convert, with: { fields: [ { name: n, type: string } ] } }
  - { id: bad, type: core.dummy, with: { nope: 1 } }
  - { id: show, type: core.log }
links:
  - { from: gen, to: conv }
  - { from: conv, to: bad }
  - { from: conv, to: show, type: error }
`)
	if diags := Check(p, Options{Registry: testRegistry(c)}); !diags.HasErrors() {
		t.Fatalf("expected an error for bad: %v", diags)
	}
	s := Schemas(p, Options{Registry: testRegistry(c)})
	names := func(sc record.Schema) string {
		var out []string
		for _, f := range sc.Fields {
			out = append(out, f.Name+":"+string(f.Type))
		}
		return strings.Join(out, ",") + fmt.Sprintf(" open=%v", sc.Open)
	}
	if s["gen"].Input != nil || names(s["gen"].Output) != "n:int open=false" {
		t.Errorf("gen %+v", s["gen"])
	}
	if s["conv"].Input == nil || names(*s["conv"].Input) != "n:int open=false" || names(s["conv"].Output) != "n:string open=false" {
		t.Errorf("conv %+v", s["conv"])
	}
	if got := names(s["conv"].Errors); got != "n:int,_error:string,_errorField:string,_errorTransform:string open=false" {
		t.Errorf("conv errors %s", got)
	}
	if !s["bad"].Output.Open || len(s["bad"].Output.Fields) != 0 {
		t.Errorf("bad %+v", s["bad"])
	}
	if s["show"].Input == nil || names(*s["show"].Input) != names(s["conv"].Errors) {
		t.Errorf("show %+v", s["show"])
	}
}
