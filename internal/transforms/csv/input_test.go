package csv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

// show writes records as one line each, with the Go type of every value.
func show(rs []record.Record) string {
	var b strings.Builder
	for _, r := range rs {
		for i, name := range r.Names() {
			v, _ := r.Get(name)
			if i > 0 {
				b.WriteString(" ")
			}
			fmt.Fprintf(&b, "%s=%v(%T)", name, v, v)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "in.csv")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, with map[string]any) ([]record.Record, error) {
	t.Helper()
	out, _, err := transformtest.Run(context.Background(), transformtest.New(t, inputDefinition, with))
	return out, err
}

func TestInputAllColumnsAsText(t *testing.T) {
	p := writeFile(t, "id,name,city\r\n1,Ada,\"Bad Homburg, Taunus\"\r\n2,\"Grace \"\"G\"\"\",\r\n")
	out, err := read(t, map[string]any{"path": p})
	if err != nil {
		t.Fatal(err)
	}
	want := "id=1(string) name=Ada(string) city=Bad Homburg, Taunus(string)\n" +
		"id=2(string) name=Grace \"G\"(string) city=(string)\n"
	if got := show(out); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestInputFieldsWithTypesAndFormats(t *testing.T) {
	p := writeFile(t, "Nr;Kunde;Betrag;Datum;Bezahlt\n7;Müller;1.234,50;19.11.2026;yes\n8;Schmidt;-0,99;01.02.2026;\n")
	out, err := read(t, map[string]any{
		"path":      p,
		"delimiter": ";",
		"fields": []any{
			map[string]any{"name": "Datum", "type": "date", "format": "dd.MM.yyyy"},
			map[string]any{"name": "Betrag", "type": "decimal", "format": "#.##0,00"},
			map[string]any{"name": "Nr", "type": "int"},
			map[string]any{"name": "Bezahlt", "type": "bool"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Only the listed columns, in the order of fields; an empty value is null.
	want := "Datum=2026-11-19(record.Date) Betrag=1234.5(decimal.Decimal) Nr=7(int64) Bezahlt=true(bool)\n" +
		"Datum=2026-02-01(record.Date) Betrag=-0.99(decimal.Decimal) Nr=8(int64) Bezahlt=<nil>(<nil>)\n"
	if got := show(out); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestInputWithoutHeader(t *testing.T) {
	p := writeFile(t, "a\tb\nc\td\n")
	out, err := read(t, map[string]any{"path": p, "delimiter": `\t`, "header": false})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := show(out), "field1=a(string) field2=b(string)\nfield1=c(string) field2=d(string)\n"; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// With fields: matched by position.
	out, err = read(t, map[string]any{"path": p, "delimiter": "tab", "header": false,
		"fields": []any{map[string]any{"name": "x"}, map[string]any{"name": "y"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := show(out), "x=a(string) y=b(string)\nx=c(string) y=d(string)\n"; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestInputEncodings(t *testing.T) {
	want := "name=Jörg(string) note=5 €(string)\n"
	for _, tt := range []struct {
		name, encoding string
		data           []byte
	}{
		{"utf-8", "", []byte("name,note\nJörg,5 €\n")},
		{"utf-8 with BOM", "", append([]byte{0xEF, 0xBB, 0xBF}, "name,note\nJörg,5 €\n"...)},
		{"utf-8 with BOM, given", "utf-8", append([]byte{0xEF, 0xBB, 0xBF}, "name,note\nJörg,5 €\n"...)},
		{"windows-1252", "windows-1252", []byte("name,note\nJ\xf6rg,5 \x80\n")},
		{"utf-16 LE with BOM", "", utf16(true, "name,note\nJörg,5 €\n")},
		{"utf-16 BE with BOM", "", utf16(false, "name,note\nJörg,5 €\n")},
		{"utf-16 LE without BOM", "utf-16", utf16(true, "name,note\nJörg,5 €\n")[2:]},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := writeFile(t, string(tt.data))
			out, err := read(t, map[string]any{"path": p, "encoding": tt.encoding})
			if err != nil {
				t.Fatal(err)
			}
			if got := show(out); got != want {
				t.Errorf("got %s, want %s", got, want)
			}
		})
	}
}

// utf16 encodes text as UTF-16 with a byte order mark.
func utf16(little bool, s string) []byte {
	var b []byte
	put := func(u uint16) {
		if little {
			b = append(b, byte(u), byte(u>>8))
		} else {
			b = append(b, byte(u>>8), byte(u))
		}
	}
	put(0xFEFF)
	for _, r := range s {
		put(uint16(r)) // only characters of the basic plane in the tests
	}
	return b
}

func TestInputErrors(t *testing.T) {
	long := strings.Repeat("x", 9000) // invalid character after the first chunk
	for _, tt := range []struct {
		name    string
		content string
		with    map[string]any
		want    string
	}{
		{"not utf-8", "name\nJ\xf6rg\n", nil,
			"the file is not valid UTF-8; if it comes from a Windows program, set encoding: windows-1252"},
		{"not utf-8 later", "name\n" + long + "\xf6\n", nil, "not valid UTF-8"},
		{"conversion", "n,amount\n1,12\n2,12a\n", map[string]any{"fields": []any{map[string]any{"name": "amount", "type": "int"}}},
			`in.csv:3:3: field "amount": "12a" is not a whole number`},
		{"missing column", "n,amont\n1,2\n", map[string]any{"fields": []any{map[string]any{"name": "amount"}}},
			`in.csv:1: the header has no column "amount" (did you mean "amont"?) (columns: n, amont)`},
		{"duplicate column", "a,a\n1,2\n", nil, `in.csv:1: the column "a" appears twice in the header`},
		{"field count", "a,b\n1,2\n3\n", nil, "in.csv:3:1: the row has a different number of values than the first row"},
		{"quote", "a,b\n1,\"x\n", nil, "in.csv:2:"},
		{"delimiter", "a\n", map[string]any{"delimiter": "${SEP}"}, `delimiter: "${SEP}" is not a single character`},
		{"encoding", "a\n", map[string]any{"encoding": "latin-9"}, `invalid encoding "latin-9"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			with := map[string]any{"path": writeFile(t, tt.content)}
			for k, v := range tt.with {
				with[k] = v
			}
			_, err := read(t, with)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %v, want %q", err, tt.want)
			}
		})
	}
	_, err := read(t, map[string]any{"path": filepath.Join(t.TempDir(), "missing.csv")})
	if err == nil || !strings.Contains(err.Error(), "cannot read") {
		t.Errorf("missing file: %v", err)
	}
}

func TestInputConfigErrors(t *testing.T) {
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"path": "x", "delimiter": ";;"}, `delimiter: ";;" is not a single character`},
		{map[string]any{"path": "x", "fields": []any{map[string]any{"name": "a", "type": "money"}}}, `field "a": invalid type "money"`},
		{map[string]any{"path": "x", "fields": []any{map[string]any{"name": "a"}, map[string]any{"name": "a"}}}, `the field "a" is listed twice`},
		{map[string]any{"path": "x", "fields": []any{map[string]any{"name": "a", "format": "0,00"}}}, "only possible for numbers and dates"},
	} {
		if _, err := inputDefinition.New(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: error %v, want %q", tt.with, err, tt.want)
		}
	}
}

func TestInputSchema(t *testing.T) {
	tr := transformtest.New(t, inputDefinition, map[string]any{"path": "x"})
	s, err := tr.(transform.Initializer).Init(transform.NewInitContext(record.Schema{}))
	if err != nil || !s.Open || len(s.Names()) != 0 {
		t.Errorf("without fields: %+v %v, want open and empty", s, err)
	}
	tr = transformtest.New(t, inputDefinition, map[string]any{"path": "x", "fields": []any{
		map[string]any{"name": "id", "type": "int"}, map[string]any{"name": "name"}}})
	s, err = tr.(transform.Initializer).Init(transform.NewInitContext(record.Schema{}))
	if err != nil || s.Open || fmt.Sprint(s.Fields) != "[{id int} {name string}]" {
		t.Errorf("with fields: %+v %v", s, err)
	}
}

func TestInputCancelled(t *testing.T) {
	var b strings.Builder
	b.WriteString("n\n")
	for i := range 10000 {
		fmt.Fprintf(&b, "%d\n", i)
	}
	tr := transformtest.New(t, inputDefinition, map[string]any{"path": writeFile(t, b.String())})
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	out := transform.NewOutput("t", func(record.Record) error {
		if n++; n == 10 {
			cancel()
		}
		return ctx.Err()
	}, nil)
	if err := tr.Process(ctx, nil, out); err == nil || n != 10 {
		t.Errorf("error %v after %d records, want cancellation after 10", err, n)
	}
}

func TestInputErrorLink(t *testing.T) {
	p := writeFile(t, "id;amount\n1;1,50\n2;viel\n3\n4;4,00\n")
	tr := transformtest.New(t, inputDefinition, map[string]any{"path": p, "delimiter": ";", "fields": []any{
		map[string]any{"name": "id", "type": "int"}, map[string]any{"name": "amount", "type": "decimal", "format": "0,00"}}})
	out, failed, logs, err := transformtest.RunWithErrorLink(context.Background(), tr)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := show(out), "id=1(int64) amount=1.5(decimal.Decimal)\nid=4(int64) amount=4(decimal.Decimal)\n"; got != want {
		t.Errorf("out\n%s\nwant\n%s", got, want)
	}
	// Rows with errors go on as text, with the error fields.
	if len(failed) != 2 {
		t.Fatalf("failed %v", failed)
	}
	for i, want := range []struct{ id, amount, msg, field string }{
		{"2", "viel", `in.csv:3:3: field "amount": "viel" is not a number`, "amount"},
		{"3", "", "in.csv:4:1: the row has a different number of values than the first row", ""},
	} {
		r := failed[i]
		id, _ := r.Get("id")
		amount, _ := r.Get("amount")
		msg, _ := r.Get("_error")
		field, _ := r.Get("_errorField")
		if id != want.id || amount != want.amount || !strings.HasSuffix(msg.(string), want.msg) || field != want.field {
			t.Errorf("failed[%d] = %s", i, show(failed[i:i+1]))
		}
	}
	if len(logs) != 1 || !strings.HasSuffix(logs[0].Msg, "in.csv, 2 with errors") || !strings.HasPrefix(logs[0].Msg, "read 4 rows") {
		t.Errorf("log %v", logs)
	}
}
