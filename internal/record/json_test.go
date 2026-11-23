package record

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestJSONValue(t *testing.T) {
	d, err := ParseDate("2026-11-23")
	if err != nil {
		t.Fatal(err)
	}
	v := map[string]any{
		"price": decimal.RequireFromString("12.50"),
		"id":    int64(9007199254740993),
		"day":   d,
		"at":    time.Date(2026, 11, 23, 8, 30, 0, 0, time.UTC),
		"list":  []any{decimal.RequireFromString("-0.1"), "a", nil, true},
		"rows":  []map[string]any{{"n": decimal.RequireFromString("1")}},
	}
	data, err := json.Marshal(JSONValue(v))
	want := `{"at":"2026-11-23T08:30:00Z","day":"2026-11-23","id":9007199254740993,"list":[-0.1,"a",null,true],"price":12.5,"rows":[{"n":1}]}`
	if err != nil || string(data) != want {
		t.Errorf("got  %s %v\nwant %s", data, err, want)
	}
	if JSONValue("text") != "text" || JSONValue(nil) != nil {
		t.Error("other values change")
	}
}
