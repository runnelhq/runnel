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

// TestDetectFieldsNDJSON: in NDJSON, dates written as text are proposed from their
// lines, keys missing in some lines and only empty values are text; fields already
// configured are kept and marked when missing.
func TestDetectFieldsNDJSON(t *testing.T) {
	p := writeFile(t, "in.ndjson", "{\"id\": 1, \"day\": \"2026-11-19\", \"none\": null}\r\n\r\n{\"id\": 2, \"day\": \"2026-11-20\", \"none\": null, \"late\": true}\r\n")
	d, err := detectFields(context.Background(), map[string]any{"path": p, "fields": []any{
		map[string]any{"name": "id", "type": "int"}, map[string]any{"name": "gone"},
	}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range d.Fields {
		got = append(got, fmt.Sprintf("%s:%s:%s:%v", f.Name, f.Type, f.State, f.Selected))
	}
	want := "id:int:listed:true gone:string:missing:true day:date:new:false none:string:new:false late:bool:new:false"
	if strings.Join(got, " ") != want {
		t.Errorf("got  %s\nwant %s", strings.Join(got, " "), want)
	}
	if d.Rows != 2 || !d.Complete || fmt.Sprint(d.Source) != "[{File "+p+"} {Format ndjson}]" {
		t.Errorf("rows %d, complete %v, source %v", d.Rows, d.Complete, d.Source)
	}
	for _, f := range d.Fields {
		if f.Name == "none" && f.Note != "only empty values; kept as text" {
			t.Errorf("note of none: %q", f.Note)
		}
	}
}

// TestDetectFieldsErrors: a broken line or a bad configuration is reported, not a
// proposal from part of the file.
func TestDetectFieldsErrors(t *testing.T) {
	p := writeFile(t, "in.ndjson", "{\"id\": 1}\n{\"id\": \n")
	if _, err := detectFields(context.Background(), map[string]any{"path": p}, 100); err == nil || !strings.Contains(err.Error(), "in.ndjson:2: ") {
		t.Errorf("broken line: %v", err)
	}
	if _, err := detectFields(context.Background(), map[string]any{"path": p, "format": "yaml"}, 100); err == nil || !strings.Contains(err.Error(), "invalid format") {
		t.Errorf("bad format: %v", err)
	}
	p = writeFile(t, "in.json", `{"data": 1}`)
	if _, err := detectFields(context.Background(), map[string]any{"path": p, "records": "data.items"}, 100); err == nil || !strings.Contains(err.Error(), "not inside an object") {
		t.Errorf("bad records: %v", err)
	}
}
