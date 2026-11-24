package plugin

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
)

// A plugin that sends more transform.emit than its window breaks its stream; notify does
// not wait for the host to take them.
func TestStreamWindow(t *testing.T) {
	pr := &Process{streams: map[int64]*stream{}}
	s := newStream(7)
	pr.streams[7] = s
	emit := json.RawMessage(`{"stream":7,"to":"main","fields":[{"name":"n","type":"int"}],"rows":[[1]]}`)
	for range EmitWindow {
		pr.notify("transform.emit", emit)
	}
	select {
	case <-s.broken:
		t.Fatal("broken within the window")
	default:
	}
	pr.notify("transform.emit", emit)
	select {
	case <-s.broken:
		if !strings.Contains(s.reason.Error(), "more than 4 transform.emit") {
			t.Errorf("reason %v", s.reason)
		}
	case <-time.After(time.Second):
		t.Fatal("not broken")
	}
	// Notifications of unknown streams and methods are ignored.
	pr.notify("transform.emit", json.RawMessage(`{"stream":8}`))
	pr.notify("demo.unknown", json.RawMessage(`{}`))
}

// Values travel as JSON by the type of their field (ADR 0019).
func TestValues(t *testing.T) {
	at := time.Date(2026, 11, 24, 10, 30, 0, 5, time.FixedZone("", 3600))
	in := record.New(
		record.Field{Name: "s", Value: "ä"}, record.Field{Name: "i", Value: int64(9007199254740993)},
		record.Field{Name: "d", Value: decimal.RequireFromString("1234.560")}, record.Field{Name: "f", Value: 0.25},
		record.Field{Name: "b", Value: true}, record.Field{Name: "day", Value: record.Date{Year: 2026, Month: 2, Day: 28}},
		record.Field{Name: "at", Value: at}, record.Field{Name: "raw", Value: []byte{0, 255}},
		record.Field{Name: "j", Value: map[string]any{"x": []any{int64(1), "y"}}}, record.Field{Name: "n", Value: nil},
	)
	data, err := json.Marshal(batchMsg{Fields: header(in), Rows: [][]any{encodeRow(in)}})
	if err != nil {
		t.Fatal(err)
	}
	var m emitWire
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		t.Fatal(err)
	}
	recs, err := decodeRows(m.Fields, m.Rows)
	if err != nil {
		t.Fatal(err)
	}
	out := recs[0]
	for _, f := range in.Fields() {
		got, _ := out.Get(f.Name)
		var ok bool
		switch want := f.Value.(type) {
		case decimal.Decimal:
			ok = want.Equal(got.(decimal.Decimal))
		case time.Time:
			ok = want.Equal(got.(time.Time))
		case []byte:
			ok = string(want) == string(got.([]byte))
		case map[string]any:
			ok = got.(map[string]any)["x"].([]any)[0] == int64(1)
		default:
			ok = got == f.Value
		}
		if !ok {
			t.Errorf("%s: %#v, want %#v", f.Name, got, f.Value)
		}
	}
	for _, tt := range []struct {
		typ   record.Type
		value any
	}{
		{record.Int, "1"}, {record.Int, json.Number("1.5")}, {record.String, json.Number("1")}, {record.Decimal, "x"},
		{record.DateType, "24.11.2026"}, {record.DateTime, "2026-11-24"}, {record.Binary, "!"}, {record.Bool, "true"},
	} {
		if _, err := decodeValue(tt.value, tt.typ); err == nil || !strings.Contains(err.Error(), "is not a value of type "+string(tt.typ)) {
			t.Errorf("%s %v: %v", tt.typ, tt.value, err)
		}
	}
	if _, err := decodeRows(m.Fields, [][]any{{1}}); err == nil || !strings.Contains(err.Error(), "row 1 has 1 values for 10 fields") {
		t.Errorf("short row: %v", err)
	}
}

// FuzzEmit: whatever a plugin sends as transform.emit, notify neither panics nor waits.
func FuzzEmit(f *testing.F) {
	f.Add(`{"stream":1,"to":"main","fields":[{"name":"n","type":"int"}],"rows":[[1]]}`)
	f.Add(`{"stream":1,"fields":[{"name":"d","type":"decimal"},{"name":"t","type":"datetime"}],"rows":[["1.5","2026-11-24T10:00:00Z"]]}`)
	f.Add(`{"stream":1,"fields":[{"name":"j","type":"json"}],"rows":[[{"a":[1,2.5,null]}]]}`)
	f.Fuzz(func(_ *testing.T, params string) {
		pr := &Process{streams: map[int64]*stream{}}
		pr.streams[1] = newStream(1)
		for range EmitWindow + 2 {
			pr.notify("transform.emit", json.RawMessage(params))
		}
		pr.notify("transform.closed", json.RawMessage(params))
		pr.notify("$/ack", json.RawMessage(params))
	})
}
