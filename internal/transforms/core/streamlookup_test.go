package core

import (
	"context"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

func rec(fields ...any) record.Record {
	r := record.New()
	for i := 0; i < len(fields); i += 2 {
		r.Set(fields[i].(string), fields[i+1])
	}
	return r
}

// Keys are compared as text: 1 from a typed file matches "1" from a CSV file. The first
// lookup record of a key wins; an empty key matches nothing.
func TestStreamLookup(t *testing.T) {
	info := []record.Record{rec("id", "1", "region", "n", "name", "Ada"), rec("id", "2", "region", "s", "name", "Grace"),
		rec("id", "2", "region", "s", "name", "Grace again"), rec("id", nil, "name", "nobody")}
	tr := transformtest.New(t, streamLookupDefinition, map[string]any{
		"keys":   []any{map[string]any{"field": "customerId", "lookupField": "id"}},
		"return": []any{map[string]any{"lookupField": "name", "as": "customer", "default": "?"}, map[string]any{"lookupField": "region"}},
	})
	out, _, logs, err := transformtest.RunWithInfo(context.Background(), tr, info,
		rec("customerId", int64(1)), rec("customerId", int64(2)), rec("customerId", nil), rec("customerId", int64(3)))
	if err != nil || len(out) != 4 {
		t.Fatalf("%d records, %v", len(out), err)
	}
	var got []string
	for _, r := range out {
		got = append(got, Format(r))
	}
	if want := `{customerId: 1, customer: "Ada", region: "n"}|{customerId: 2, customer: "Grace", region: "s"}|{customerId: null, customer: "?", region: null}|{customerId: 3, customer: "?", region: null}`; strings.Join(got, "|") != want {
		t.Errorf("records\n%s\nwant\n%s", strings.Join(got, "|"), want)
	}
	if logs[1].Msg != "loaded 4 lookup records from lookupdata (2 keys)" || logs[2].Msg != "looked up 4 records: 2 found, 2 not found" {
		t.Errorf("logs %v", logs)
	}

	// Two keys; several matches as error; drop without a match.
	tr = transformtest.New(t, streamLookupDefinition, map[string]any{
		"keys":     []any{map[string]any{"field": "id"}, map[string]any{"field": "region"}},
		"return":   []any{map[string]any{"lookupField": "name"}},
		"multiple": "error", "notFound": "drop",
	})
	out, failed, _, err := transformtest.RunWithInfo(context.Background(), tr, info,
		rec("id", "1", "region", "n"), rec("id", "2", "region", "s"), rec("id", "1", "region", "s"))
	if err != nil || len(out) != 1 || len(failed) != 1 {
		t.Fatalf("%d out, %d failed, %v", len(out), len(failed), err)
	}
	if v, _ := failed[0].Get(transform.ErrorField); v != "record 2: several lookup records from lookupdata match (id = 2, region = s)" {
		t.Errorf("error %v", v)
	}
}

func TestStreamLookupConfig(t *testing.T) {
	keys := []any{map[string]any{"field": "id"}}
	ret := []any{map[string]any{"lookupField": "name"}}
	for want, with := range map[string]map[string]any{
		`missing required field "keys"`:                  {"return": ret},
		"keys: name at least one field":                  {"keys": []any{}, "return": ret},
		"return: name at least one field":                {"keys": keys, "return": []any{}},
		`missing required field "return[0].lookupField"`: {"keys": keys, "return": []any{map[string]any{"as": "x"}}},
		`invalid notFound "skip"`:                        {"keys": keys, "return": ret, "notFound": "skip"},
		`invalid multiple "all"`:                         {"keys": keys, "return": ret, "multiple": "all"},
		"maxRecords must be at least 1":                  {"keys": keys, "return": ret, "maxRecords": 0},
	} {
		if _, err := streamLookupDefinition.New(with); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: %v, want %q", with, err, want)
		}
	}
	// Without an info link (outside the engine) Process reports it.
	tr := transformtest.New(t, streamLookupDefinition, map[string]any{"keys": keys, "return": ret})
	if _, _, err := transformtest.Run(context.Background(), tr); err == nil || err.Error() != "core.stream-lookup needs an info link with the lookup data" {
		t.Errorf("%v", err)
	}
	// Init without an info link: the lookup fields are not known yet.
	ic := transform.NewInitContext(record.Schema{}.With("id", record.Int))
	_, _ = tr.(transform.Initializer).Init(ic)
	if p := ic.Problems(); len(p) != 2 || p[0].Message != `field "id" of the lookup data is not known: there is no info link yet` || !p[0].Warning {
		t.Errorf("%+v", p)
	}
}

// BenchmarkStreamLookup looks up 100,000 records among 10,000 lookup records.
func BenchmarkStreamLookup(b *testing.B) {
	info := make([]record.Record, 10000)
	for i := range info {
		info[i] = rec("id", int64(i), "name", "customer")
	}
	input := make([]record.Record, 100000)
	for i := range input {
		input[i] = rec("customerId", int64(i%20000))
	}
	b.ReportAllocs()
	for b.Loop() {
		tr, _ := streamLookupDefinition.New(map[string]any{
			"keys": []any{map[string]any{"field": "customerId", "lookupField": "id"}}, "return": []any{map[string]any{"lookupField": "name"}}})
		recs := make([]record.Record, len(input))
		for i, r := range input {
			recs[i] = r.Clone()
		}
		if _, _, _, err := transformtest.RunWithInfo(context.Background(), tr, info, recs...); err != nil {
			b.Fatal(err)
		}
	}
}
