package json

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestDetectFields(t *testing.T) {
	p := writeFile(t, "in.json", `{"items": [
  {"id": 1, "amount": 2, "day": "19.11.2026", "paid": true, "address": {"city": "Köln"}, "code": "0221"},
  {"id": 2, "amount": 2.5, "day": "20.11.2026", "paid": false, "mixed": 1, "code": "0228"},
  {"id": 3, "amount": 3, "day": "21.11.2026", "mixed": "x"}
]}`)
	d, err := detectFields(context.Background(), map[string]any{"path": p, "records": "items"}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range d.Fields {
		got = append(got, fmt.Sprintf("%s:%s:%s:%v", f.Name, f.Type, f.Format, f.Warning))
	}
	want := "id:int::false amount:decimal::false day:date:dd.MM.yyyy:false paid:bool::false address:json::false code:string::false mixed:string::true"
	if strings.Join(got, " ") != want {
		t.Errorf("got  %s\nwant %s", strings.Join(got, " "), want)
	}
	if d.Rows != 3 || !d.Complete {
		t.Errorf("rows %d, complete %v", d.Rows, d.Complete)
	}
	if v := d.Fields[0].Values; fmt.Sprint(v) != "[1 2 3]" {
		t.Errorf("values %v", v)
	}
	d, err = detectFields(context.Background(), map[string]any{"path": p, "records": "items"}, 2)
	if err != nil || d.Rows != 2 || d.Complete {
		t.Errorf("limit: %+v %v", d, err)
	}
}
