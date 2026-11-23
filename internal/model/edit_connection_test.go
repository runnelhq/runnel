package model

import (
	"strings"
	"testing"
)

const connText = `# The ERP database.
kind: Connection
apiVersion: runnel/v1
name: erp
type: sqlserver
with:
  host: ${ERP_HOST}   # from the environment
  database: ERP
  password: ${secret:erp-pw}
`

func TestEditConnection(t *testing.T) {
	cases := []struct {
		f    ConnectionField
		want string
	}{
		{ConnectionField{Field: "with.host", Value: "db01"}, "  host: db01   # from the environment\n"},
		{ConnectionField{Field: "with.port", Value: "1433"}, "  password: ${secret:erp-pw}\n  port: \"1433\"\n"},
		{ConnectionField{Field: "with.encrypt", Value: false}, "  encrypt: false\n"},
		{ConnectionField{Field: "with.password", Value: nil}, "  database: ERP\n"},
		{ConnectionField{Field: "description", Value: "ERP, read only"}, "name: erp\ndescription: ERP, read only\ntype: sqlserver\n"},
		{ConnectionField{Field: "type", Value: "postgres"}, "type: postgres\n"},
		{ConnectionField{Field: "with.headers", Map: []KeyValue{{Key: "Accept", Value: "application/json"}}}, "  headers:\n    Accept: application/json\n"},
	}
	for _, c := range cases {
		out, err := EditConnection("connections/erp.yaml", []byte(connText), c.f)
		if err != nil {
			t.Fatalf("%+v: %v", c.f, err)
		}
		if !strings.Contains(string(out), c.want) || !strings.HasPrefix(string(out), "# The ERP database.\n") {
			t.Errorf("%+v:\n%s\nwant containing %q", c.f, out, c.want)
		}
		if c.f.Field == "with.password" && strings.Contains(string(out), "password") {
			t.Errorf("not removed:\n%s", out)
		}
	}
}

func TestEditConnectionNewWithAndCRLF(t *testing.T) {
	text := strings.ReplaceAll("kind: Connection\napiVersion: runnel/v1\nname: api\ntype: http\n", "\n", "\r\n")
	out, err := EditConnection("connections/api.yaml", []byte(text), ConnectionField{Field: "with.baseUrl", Value: "https://h/"})
	if err != nil || string(out) != text+"with:\r\n  baseUrl: https://h/\r\n" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestEditConnectionRefusals(t *testing.T) {
	for _, c := range []struct {
		f    ConnectionField
		want string
	}{
		{ConnectionField{Field: "name", Value: "x"}, "cannot be set"},
		{ConnectionField{Field: "with.a b", Value: "x"}, "cannot be set"},
		{ConnectionField{Field: "type", Value: nil}, "cannot be removed"},
		{ConnectionField{Field: "kind.x", Value: "x"}, "cannot be set"},
	} {
		if _, err := EditConnection("connections/erp.yaml", []byte(connText), c.f); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v", c.f, err)
		}
	}
	if _, err := EditConnection("connections/erp.yaml", []byte("with: {host: a}\nname: x\n"), ConnectionField{Field: "with.host", Value: "b"}); err == nil || !strings.Contains(err.Error(), "flow style") {
		t.Errorf("flow style: %v", err)
	}
	if _, err := EditConnection("connections/erp.yaml", []byte("kind: [\n"), ConnectionField{Field: "description", Value: "b"}); err == nil {
		t.Error("broken YAML accepted")
	}
}
