package pipeline

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/record"
)

// lookupPipeline: 50 customers as lookup data over an info link, 100 orders looked up.
const lookupPipeline = `transforms:
  - { id: customers, type: core.generate-rows, with: { count: 50, numberField: id } }
  - { id: names, type: core.calculate, with: { fields: [ { name: name, value: "c{{ record.id }}" } ] } }
  - { id: orders, type: core.generate-rows, with: { count: 100, numberField: customerId } }
  - { id: lookup, type: core.stream-lookup, with: { keys: [ { field: customerId, lookupField: id } ], return: [ { lookupField: name, as: customer, default: unknown } ] %s } }
  - { id: out, type: test.collect }
  - { id: bad, type: test.collect }
links:
  - { from: customers, to: names }
  - { from: names, to: lookup, type: info }
  - { from: orders, to: lookup }
  - { from: lookup, to: out }
  - { from: lookup, to: bad, type: error }
`

// The info link is read completely first, with buffers much smaller than the lookup data
// and the orders; its records count apart (Counts.Info) and on the link.
func TestRunInfoLink(t *testing.T) {
	res, c, events := run(context.Background(), t, fmt.Sprintf(lookupPipeline, ""), nil)
	if res.Status != Succeeded {
		t.Fatalf("%s: %v", res.Status, res.Err)
	}
	if got := res.Counts["lookup"]; got != (Counts{Read: 100, Written: 100, Info: 50}) {
		t.Errorf("counts %+v", got)
	}
	if got := res.LinkRows[1]; got != 50 {
		t.Errorf("records on the info link: %d", got)
	}
	recs := c.recs["out"]
	if len(recs) != 100 {
		t.Fatalf("%d records", len(recs))
	}
	for _, i := range []int{0, 49, 50, 99} {
		want := fmt.Sprintf("c%d", i+1)
		if i >= 50 {
			want = "unknown"
		}
		if v, _ := recs[i].Get("customer"); v != want {
			t.Errorf("record %d: customer = %v, want %s", i+1, v, want)
		}
	}
	var logs []string
	for _, e := range events {
		if e.Kind == Log && e.TransformID == "lookup" {
			logs = append(logs, e.Message)
		}
	}
	if got := strings.Join(logs, "\n"); got != "waiting for the lookup data from names (info link)\nloaded 50 lookup records from names (50 keys)\nlooked up 100 records: 50 found, 50 not found" {
		t.Errorf("log:\n%s", got)
	}

	// notFound: error sends the orders without a customer to the error link.
	res, c, _ = run(context.Background(), t, fmt.Sprintf(lookupPipeline, ", notFound: error"), nil)
	if res.Status != Succeeded || len(c.recs["out"]) != 50 || len(c.recs["bad"]) != 50 {
		t.Fatalf("%s %v: %d out, %d bad", res.Status, res.Err, len(c.recs["out"]), len(c.recs["bad"]))
	}
	if v, _ := c.recs["bad"][0].Get("_error"); v != "record 51: no lookup record from names matches (customerId = 51)" {
		t.Errorf("error: %v", v)
	}

	// More lookup data than maxRecords stops the pipeline.
	res, _, _ = run(context.Background(), t, fmt.Sprintf(lookupPipeline, ", maxRecords: 10"), nil)
	if res.Status != Failed || res.Err.Error() != `transform "lookup": the lookup data from names has more than 10 records (maxRecords); for larger tables use db.lookup` {
		t.Errorf("maxRecords: %s %v", res.Status, res.Err)
	}
}

// The schema of the info link is passed to Init: returned fields get its types, and lookup
// fields are checked against it.
func TestPrepareInfoLinkSchemas(t *testing.T) {
	c := &collected{recs: map[string][]record.Record{}}
	p := parse(t, strings.Replace(fmt.Sprintf(lookupPipeline, ""), "lookupField: name, as: customer", "lookupField: nam, as: customer", 1))
	var msgs []string
	for _, d := range Check(p, Options{Registry: testRegistry(c)}) {
		msgs = append(msgs, d.Error())
	}
	if want := `transform "lookup": field "nam" does not exist in the lookup data (did you mean "name"?) (fields: id, name)`; len(msgs) != 1 || !strings.Contains(msgs[0], want) {
		t.Errorf("problems %q, want %q", msgs, want)
	}
	s := Schemas(parse(t, fmt.Sprintf(lookupPipeline, "")), Options{Registry: testRegistry(c)})["lookup"]
	if s.Info == nil || strings.Join(s.Info.Names(), ",") != "id,name" {
		t.Errorf("info schema %+v", s.Info)
	}
	if strings.Join(s.Input.Names(), ",") != "customerId" {
		t.Errorf("input schema %+v (the info link is not merged)", s.Input)
	}
	if f, _ := s.Output.Field("customer"); f.Type != record.String {
		t.Errorf("customer: %v", f.Type)
	}
}

func TestPrepareInfoLinkErrors(t *testing.T) {
	c := &collected{recs: map[string][]record.Record{}}
	lookup := "{ id: lookup, type: core.stream-lookup, with: { keys: [ { field: n } ], return: [ { lookupField: x } ] } }"
	for name, tc := range map[string]struct{ body, want string }{
		"no info link": {`transforms:
  - { id: gen, type: core.generate-rows, with: { count: 1, numberField: n } }
  - ` + lookup + `
links:
  - { from: gen, to: lookup }
`, `transform "lookup": core.stream-lookup needs an info link with the lookup data`},
		"not a lookup": {`transforms:
  - { id: gen, type: core.generate-rows, with: { count: 1, numberField: n } }
  - { id: other, type: core.generate-rows, with: { count: 1 } }
  - { id: pass, type: core.dummy }
links:
  - { from: gen, to: pass }
  - { from: other, to: pass, type: info }
`, `link #2 (other → pass): core.dummy takes no info link; only transforms for lookups do, e.g. core.stream-lookup`},
		"two info links": {`transforms:
  - { id: gen, type: core.generate-rows, with: { count: 1, numberField: n } }
  - { id: a, type: core.generate-rows, with: { count: 1, numberField: n } }
  - { id: b, type: core.generate-rows, with: { count: 1, numberField: n } }
  - ` + lookup + `
links:
  - { from: gen, to: lookup }
  - { from: a, to: lookup, type: info }
  - { from: b, to: lookup, type: info }
`, `link #3 (b → lookup): lookup takes one info link; link #2 already brings its lookup data`},
		"same source": {`transforms:
  - { id: gen, type: core.generate-rows, with: { count: 1, numberField: n } }
  - { id: pass, type: core.dummy }
  - ` + lookup + `
links:
  - { from: gen, to: pass }
  - { from: pass, to: lookup }
  - { from: gen, to: lookup, type: info }
`, `link #3 (gen → lookup): the lookup data and the records of link #2 both come from "gen"; lookup reads the lookup data completely first, so they must come from separate transforms`},
	} {
		var msgs []string
		for _, d := range Check(parse(t, tc.body), Options{Registry: testRegistry(c)}) {
			msgs = append(msgs, d.Error())
		}
		if got := strings.Join(msgs, "\n"); !strings.Contains(got, tc.want) {
			t.Errorf("%s: missing %q in:\n%s", name, tc.want, got)
		}
	}
}
