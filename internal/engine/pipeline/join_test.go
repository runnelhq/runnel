package pipeline

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/record"
)

// joinPipeline: 1,000 orders (two per customer, customers 0 to 499) and 400 customers
// (100 to 499), joined with tiny buffers, so that the two inputs must be read in step.
const joinPipeline = `transforms:
  - { id: orders, type: core.generate-rows, with: { count: 1000, numberField: n } }
  - { id: ordersKey, type: core.calculate, with: { fields: [ { name: customerId, value: "{{ int((record.n - 1) / 2) }}" } ] } }
  - { id: customers, type: core.generate-rows, with: { count: 400, numberField: n } }
  - { id: customersKey, type: core.calculate, with: { fields: [ { name: id, value: "{{ record.n + 99 }}" } ] } }
  - { id: join, type: core.merge-join, with: { left: ordersKey, right: customersKey, keys: [ { left: customerId, right: id } ], join: %s } }
  - { id: out, type: test.collect }
links:
  - { from: orders, to: ordersKey }
  - { from: customers, to: customersKey }
  - { from: ordersKey, to: join }
  - { from: customersKey, to: join }
  - { from: join, to: out }
`

func TestRunMergeJoin(t *testing.T) {
	for join, want := range map[string]int{"inner": 800, "left": 1000, "full": 1000} {
		res, c, _ := run(context.Background(), t, fmt.Sprintf(joinPipeline, join), nil)
		if res.Status != Succeeded {
			t.Fatalf("%s: %s %v", join, res.Status, res.Err)
		}
		if got := len(c.recs["out"]); got != want {
			t.Errorf("%s: %d records, want %d", join, got, want)
		}
		if got := res.Counts["join"]; got.Read != 1400 || got.Written != int64(want) {
			t.Errorf("%s: counts %+v", join, got)
		}
	}
}

func TestPrepareMergeJoin(t *testing.T) {
	c := &collected{recs: map[string][]record.Record{}}
	for name, tc := range map[string]struct{ body, want string }{
		"wrong source": {strings.Replace(fmt.Sprintf(joinPipeline, "inner"), "right: customersKey", "right: customers", 1),
			`transform "join": no data link comes from "customers" (incoming: customersKey, ordersKey)`},
		"same source": {`transforms:
  - { id: gen, type: core.generate-rows, with: { count: 1, numberField: n } }
  - { id: a, type: core.dummy }
  - { id: b, type: core.dummy }
  - { id: join, type: core.merge-join, with: { left: a, right: b, keys: [ { left: n } ] } }
links:
  - { from: gen, to: a }
  - { from: gen, to: b }
  - { from: a, to: join }
  - { from: b, to: join }
`, `link #4 (b → join): its records and those of link #3 both come from "gen"; join reads its inputs in step, so they must come from separate transforms`},
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
