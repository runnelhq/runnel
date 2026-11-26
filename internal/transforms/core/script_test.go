package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

func runScript(t *testing.T, with map[string]any, in ...record.Record) (out, failed []record.Record, logs []transformtest.Logged, err error) {
	t.Helper()
	tr := transformtest.New(t, scriptDefinition, with)
	return transformtest.RunWithErrorLink(context.Background(), tr, in...)
}

// types returns the Go types of the fields of a record, e.g. "a:int64 b:string".
func types(r record.Record) string {
	var parts []string
	for _, f := range r.Fields() {
		parts = append(parts, fmt.Sprintf("%s:%T", f.Name, f.Value))
	}
	return strings.Join(parts, " ")
}

func TestScriptTransformConfig(t *testing.T) {
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{}, "give the code in script or its file in file"},
		{map[string]any{"script": "return record", "file": "scripts/a.js"}, "either script or file"},
		{map[string]any{"file": "scripts/a.ps1"}, "expected a .js file"},
		{map[string]any{"script": "return ]"}, "script, line 1: syntax error"},
		{map[string]any{"script": "return record", "init": "let x = ;"}, "init, line 1: syntax error"},
		{map[string]any{"script": "return record", "end": "return ]"}, "end, line 1: syntax error"},
		{map[string]any{"script": "return record", "maxRecordTime": "soon"}, `invalid maxRecordTime "soon"`},
		{map[string]any{"script": "return record", "maxRecordTime": "-1s"}, "invalid maxRecordTime"},
		{map[string]any{"script": "return record", "fields": []any{map[string]any{"name": "", "type": "int"}}}, "fields[0]: every field needs a name"},
		{map[string]any{"script": "return record", "fields": []any{map[string]any{"name": "a", "type": "int"}, map[string]any{"name": "a", "type": "int"}}}, "listed twice"},
		{map[string]any{"script": "return record", "fields": []any{map[string]any{"name": "a", "type": "money"}}}, "fields[0] (a)"},
	} {
		if _, err := newScript(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.with, err, tt.want)
		}
	}
	// A variable in maxRecordTime is checked when the pipeline runs.
	tr := transformtest.New(t, scriptDefinition, map[string]any{"script": "return record", "maxRecordTime": "${LIMIT}"})
	if _, _, err := transformtest.Run(context.Background(), tr); err == nil || !strings.Contains(err.Error(), "invalid maxRecordTime") {
		t.Errorf("variable: %v", err)
	}
}

// Values go into the script as in ADR 0028 §4 and come back by their JavaScript type
// without fields.
func TestScriptTransformTypes(t *testing.T) {
	when := time.Date(2026, 11, 26, 8, 30, 0, 0, time.UTC)
	in := record.New(
		record.Field{Name: "s", Value: "a"}, record.Field{Name: "i", Value: int64(42)}, record.Field{Name: "big", Value: int64(1) << 60},
		record.Field{Name: "f", Value: 1.5}, record.Field{Name: "d", Value: decimal.RequireFromString("12.50")},
		record.Field{Name: "b", Value: true}, record.Field{Name: "date", Value: record.Date{Year: 2026, Month: 11, Day: 26}},
		record.Field{Name: "dt", Value: when}, record.Field{Name: "bin", Value: []byte{1, 2}},
		record.Field{Name: "j", Value: map[string]any{"a": []any{int64(1)}}}, record.Field{Name: "n", Value: nil},
	)
	out, failed, _, err := runScript(t, map[string]any{"script": `
		const t = v => v === null ? "null" : v instanceof Date ? "Date" : v instanceof Uint8Array ? "Uint8Array" : typeof v;
		const seen = Object.keys(record).map(k => k + ":" + t(record[k])).join(" ");
		return { seen, ...record, dPlus: record.d + "!", whole: 3.0, half: 0.5, made: new Date(Date.UTC(2026, 0, 2)),
			bytes: new Uint8Array([7]), buf: new Uint8Array([8, 9]).buffer, list: [1, "x"], none: undefined, big2: 5n };`}, in)
	if err != nil || len(failed) > 0 || len(out) != 1 {
		t.Fatalf("%v %v %v", out, failed, err)
	}
	r := out[0]
	if seen, _ := r.Get("seen"); seen != "s:string i:number big:bigint f:number d:string b:boolean date:string dt:Date bin:Uint8Array j:object n:null" {
		t.Errorf("in the script: %v", seen)
	}
	want := "seen:string s:string i:int64 big:int64 f:float64 d:string b:bool date:string dt:time.Time bin:[]uint8 j:map[string]interface {} n:<nil> " +
		"dPlus:string whole:int64 half:float64 made:time.Time bytes:[]uint8 buf:[]uint8 list:[]interface {} none:<nil> big2:int64"
	if got := types(r); got != want {
		t.Errorf("types\n got %s\nwant %s", got, want)
	}
	for name, want := range map[string]any{"big": int64(1) << 60, "d": "12.50", "dPlus": "12.50!", "date": "2026-11-26", "buf": []byte{8, 9}, "list": []any{int64(1), "x"}} {
		if v, _ := r.Get(name); !reflect.DeepEqual(v, want) {
			t.Errorf("%s = %#v, want %#v", name, v, want)
		}
	}
	if v, _ := r.Get("dt"); !v.(time.Time).Equal(when) {
		t.Errorf("dt = %v", v)
	}
}

// With fields, the values are converted to the declared types in their order; missing
// ones are null, others are dropped with one warning; a value that does not fit is an
// error of the record.
func TestScriptTransformFields(t *testing.T) {
	with := map[string]any{
		"fields": []any{
			map[string]any{"name": "amount", "type": "decimal"}, map[string]any{"name": "day", "type": "date"},
			map[string]any{"name": "count", "type": "int"}, map[string]any{"name": "rate", "type": "float"},
			map[string]any{"name": "at", "type": "datetime"}, map[string]any{"name": "ok", "type": "bool"},
			map[string]any{"name": "data", "type": "binary"}, map[string]any{"name": "doc", "type": "json"},
			map[string]any{"name": "label", "type": "string"}, map[string]any{"name": "missing", "type": "string"},
		},
		"script": `return { extra: 1, label: 7, doc: { a: 1 }, data: new Uint8Array([1]), ok: true, at: new Date(0),
			rate: 2, count: record.count, day: "2026-11-26", amount: record.amount, extra2: 2 };`,
	}
	in := []record.Record{
		record.New(record.Field{Name: "amount", Value: decimal.RequireFromString("12.50")}, record.Field{Name: "count", Value: "3"}),
		record.New(record.Field{Name: "amount", Value: "x"}, record.Field{Name: "count", Value: int64(1)}),
		record.New(record.Field{Name: "amount", Value: "1"}, record.Field{Name: "count", Value: 1.5}),
	}
	out, failed, logs, err := runScript(t, with, in...)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || len(failed) != 2 {
		t.Fatalf("%d out, %d failed", len(out), len(failed))
	}
	want := "amount:decimal.Decimal day:record.Date count:int64 rate:float64 at:time.Time ok:bool data:[]uint8 doc:map[string]interface {} label:string missing:<nil>"
	if got := types(out[0]); got != want {
		t.Errorf("types\n got %s\nwant %s", got, want)
	}
	if v, _ := out[0].Get("amount"); v.(decimal.Decimal).String() != "12.5" {
		t.Errorf("amount %v", v)
	}
	if v, _ := out[0].Get("label"); v != "7" {
		t.Errorf("label %v", v)
	}
	// The failed record is the one that came in, with the message and the field.
	if !reflect.DeepEqual(failed[0].Names()[:2], []string{"amount", "count"}) {
		t.Errorf("failed record %v", failed[0].Names())
	}
	if msg, _ := failed[0].Get("_error"); !strings.HasPrefix(fmt.Sprint(msg), `record 2: field "amount": `) {
		t.Errorf("error %v", msg)
	}
	if f, _ := failed[1].Get("_errorField"); f != "count" {
		t.Errorf("error field %v", f)
	}
	var warnings []string
	for _, l := range logs {
		if strings.Contains(l.Msg, "not listed in fields") {
			warnings = append(warnings, l.Msg)
		}
	}
	if len(warnings) != 2 || !strings.Contains(warnings[0], `"extra"`) {
		t.Errorf("warnings %v", warnings)
	}

	// binary only from bytes.
	out, failed, _, _ = runScript(t, map[string]any{"fields": []any{map[string]any{"name": "data", "type": "binary"}}, "script": "return { data: 'x' }"}, record.New())
	if len(out) != 0 || len(failed) != 1 {
		t.Errorf("binary from text: %v %v", out, failed)
	}
}

// An object is one record, a list several, null and undefined none; other values are
// errors of the record.
func TestScriptTransformResults(t *testing.T) {
	in := []record.Record{}
	for _, n := range []int64{0, 1, 2, 3, 4, 5} {
		in = append(in, record.New(record.Field{Name: "n", Value: n}))
	}
	out, failed, _, err := runScript(t, map[string]any{"script": `
		switch (record.n) {
		case 0: return null;
		case 1: return;
		case 2: return [{ n: 2, i: 1 }, { n: 2, i: 2 }];
		case 3: return "text";
		case 4: return [{ n: 4 }, 5];
		}
		return { z: 1, a: record.n }; // the order of the object`}, in...)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range out {
		got = append(got, Format(r))
	}
	if want := []string{"{n: 2, i: 1}", "{n: 2, i: 2}", "{z: 1, a: 5}"}; !reflect.DeepEqual(got, want) {
		t.Errorf("records %v", got)
	}
	if len(failed) != 2 {
		t.Fatalf("failed %v", failed)
	}
	for i, want := range []string{"record 4: script: return an object, a list of objects or null, not a text", "record 5: script: item 1 of the returned list: return an object, a list of objects or null, not a number"} {
		if msg, _ := failed[i].Get("_error"); msg != want {
			t.Errorf("error %q, want %q", msg, want)
		}
	}
}

// An exception sends the record unchanged to the error link with its message and line;
// without an error link, the pipeline fails with it.
func TestScriptTransformException(t *testing.T) {
	with := map[string]any{"script": "record.n = 99;\nif (record.bad) {\n  throw new Error('bad ' + record.id);\n}\nreturn record;"}
	in := []record.Record{
		record.New(record.Field{Name: "id", Value: "a"}),
		record.New(record.Field{Name: "id", Value: "b"}, record.Field{Name: "bad", Value: true}),
		record.New(record.Field{Name: "id", Value: "c"}),
	}
	out, failed, logs, err := runScript(t, with, in...)
	if err != nil || len(out) != 2 || len(failed) != 1 {
		t.Fatalf("%v %v %v", out, failed, err)
	}
	if Format(failed[0]) != `{id: "b", bad: true, _error: "record 2: script, line 3: bad b", _errorField: "", _errorTransform: "t"}` {
		t.Errorf("failed %s", Format(failed[0]))
	}
	if len(logs) != 1 || logs[0].Msg != "1 of 3 records with errors" {
		t.Errorf("logs %v", logs)
	}
	tr := transformtest.New(t, scriptDefinition, with)
	if _, _, err := transformtest.Run(context.Background(), tr, in...); err == nil || err.Error() != "record 2: script, line 3: bad b" {
		t.Errorf("without error link: %v", err)
	}
}

// A record that takes longer than maxRecordTime is an error of the record; the runtime
// goes on with the next one.
func TestScriptTransformRecordTime(t *testing.T) {
	with := map[string]any{"maxRecordTime": "50ms", "script": "if (record.loop) { while (true) { try { while (true) {} } catch (e) {} } }\nreturn record;"}
	start := time.Now()
	out, failed, _, err := runScript(t, with,
		record.New(record.Field{Name: "loop", Value: true}), record.New(record.Field{Name: "n", Value: int64(1)}),
		record.New(record.Field{Name: "loop", Value: true}), record.New(record.Field{Name: "n", Value: int64(2)}))
	if err != nil || len(out) != 2 || len(failed) != 2 {
		t.Fatalf("%v %v %v", out, failed, err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %s", d)
	}
	if msg, _ := failed[1].Get("_error"); msg != "record 3: script: the script took longer than maxRecordTime (50ms)" {
		t.Errorf("error %v", msg)
	}
	// Quick records are not affected by the timer of an earlier one.
	in := make([]record.Record, 2000)
	for i := range in {
		in[i] = record.New(record.Field{Name: "n", Value: int64(i)})
	}
	if out, failed, _, err := runScript(t, map[string]any{"maxRecordTime": "1ms", "script": "return record"}, in...); err != nil || len(out)+len(failed) != len(in) {
		t.Errorf("quick records: %d %d %v", len(out), len(failed), err)
	}
}

// Cancelling the run interrupts a running script at once.
func TestScriptTransformCancel(t *testing.T) {
	tr := transformtest.New(t, scriptDefinition, map[string]any{"maxRecordTime": "1h", "script": "while (true) {}"})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	done := make(chan error, 1)
	go func() {
		_, _, _, err := transformtest.RunWithErrorLink(ctx, tr, record.New())
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("not interrupted")
	}
}

// init runs once at the top level: its variables are seen by script and end; end may
// return further records. params are there in all of them.
func TestScriptTransformInitEnd(t *testing.T) {
	with := map[string]any{
		"params": map[string]any{"factor": 2, "label": "sum"},
		"init":   "let count = 0;\nvar total = 0;\nfunction double(x) { return x * params.factor; }",
		"script": "count++;\ntotal += double(record.n);\nconsole.log('record', count);\nreturn { n: record.n, count };",
		"end":    "return [{ label: params.label, total, count }];",
	}
	var in []record.Record
	for i := range 3 {
		in = append(in, record.New(record.Field{Name: "n", Value: int64(i + 1)}))
	}
	out, _, logs, err := runScript(t, with, in...)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range out {
		got = append(got, Format(r))
	}
	if want := []string{"{n: 1, count: 1}", "{n: 2, count: 2}", "{n: 3, count: 3}", `{label: "sum", total: 12, count: 3}`}; !reflect.DeepEqual(got, want) {
		t.Errorf("records %v", got)
	}
	if len(logs) != 3 || logs[2].Msg != "record 3" {
		t.Errorf("logs %v", logs)
	}
	// An exception in init or end stops the pipeline.
	for _, w := range []map[string]any{{"script": "return record", "init": "throw new Error('no')"}, {"script": "return record", "end": "\nthrow new Error('no')"}} {
		tr := transformtest.New(t, scriptDefinition, w)
		if _, _, _, err := transformtest.RunWithErrorLink(context.Background(), tr, in...); err == nil || !strings.Contains(err.Error(), "line") || !strings.Contains(err.Error(), ": no") {
			t.Errorf("%v: %v", w, err)
		}
	}
}

// console.* is limited per run of the transform, not per record.
func TestScriptTransformConsoleLimit(t *testing.T) {
	in := make([]record.Record, 1500)
	for i := range in {
		in[i] = record.New()
	}
	_, _, logs, err := runScript(t, map[string]any{"script": "console.log('x'); return null;"}, in...)
	if err != nil || len(logs) != 1001 || !strings.Contains(logs[1000].Msg, "more than 1000 lines") {
		t.Errorf("%d lines, %v", len(logs), err)
	}
}

// A file and modules come from the project folder (the base of the run).
func TestScriptTransformFile(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{
		"scripts/clean.js": "\uFEFFconst u = require('./scripts/util.js');\nreturn { name: u.clean(record.name) };",
		"scripts/util.js":  "exports.clean = s => s.trim().toUpperCase();",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx := paths.WithBase(context.Background(), dir)
	tr := transformtest.New(t, scriptDefinition, map[string]any{"file": "scripts/clean.js"})
	out, _, err := transformtest.Run(ctx, tr, record.New(record.Field{Name: "name", Value: " ada "}))
	if err != nil || len(out) != 1 || Format(out[0]) != `{name: "ADA"}` {
		t.Errorf("%v %v", out, err)
	}
	tr = transformtest.New(t, scriptDefinition, map[string]any{"file": "scripts/missing.js"})
	if _, _, err := transformtest.Run(ctx, tr); err == nil || err.Error() != "script file scripts/missing.js not found" {
		t.Errorf("missing: %v", err)
	}
	tr = transformtest.New(t, scriptDefinition, map[string]any{"script": "return require('../x.js')"})
	if _, _, err := transformtest.Run(ctx, tr, record.New()); err == nil || !strings.Contains(err.Error(), "only .js files in the folder scripts/") {
		t.Errorf("outside: %v", err)
	}
}

// With fields the schema is closed, without it is open.
func TestScriptTransformSchema(t *testing.T) {
	in := record.Schema{Fields: []record.FieldDef{{Name: "a", Type: record.String}}}
	tr := transformtest.New(t, scriptDefinition, map[string]any{"script": "return record"})
	if s, err := tr.(transform.Initializer).Init(transform.NewInitContext(in)); err != nil || !s.Open || len(s.Fields) != 0 {
		t.Errorf("without fields: %+v %v", s, err)
	}
	tr = transformtest.New(t, scriptDefinition, map[string]any{"script": "return record", "fields": []any{
		map[string]any{"name": "x", "type": "decimal"}, map[string]any{"name": "y", "type": "binary"}}})
	s, err := tr.(transform.Initializer).Init(transform.NewInitContext(in))
	if want := (record.Schema{Fields: []record.FieldDef{{Name: "x", Type: record.Decimal}, {Name: "y", Type: record.Binary}}}); err != nil || !reflect.DeepEqual(s, want) {
		t.Errorf("with fields: %+v %v", s, err)
	}
}

// BenchmarkScriptTransform runs 100,000 records with 5 fields through a simple script
// (ADR 0028: more than 100,000 records/s).
func BenchmarkScriptTransform(b *testing.B) {
	const n = 100_000
	with := map[string]any{"script": "return { ...record, total: Number(record.amount) * 2 };"}
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		tr, err := newScript(with)
		if err != nil {
			b.Fatal(err)
		}
		in := make(chan transform.Batch, n/1000)
		for i := range n / 1000 {
			batch := make(transform.Batch, 1000)
			for j := range batch {
				batch[j] = record.New(record.Field{Name: "id", Value: int64(i*1000 + j)}, record.Field{Name: "name", Value: "pen"},
					record.Field{Name: "amount", Value: decimal.RequireFromString("1.10")}, record.Field{Name: "ok", Value: true},
					record.Field{Name: "qty", Value: 3.5})
			}
			in <- batch
		}
		close(in)
		b.StartTimer()
		count := 0
		out := transform.NewOutput("t", func(record.Record) error { count++; return nil }, nil)
		if err := tr.Process(context.Background(), in, out); err != nil || count != n {
			b.Fatal(count, err)
		}
	}
	b.ReportMetric(float64(n*b.N)/b.Elapsed().Seconds(), "records/s")
}
