package plugin

import (
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/record"
)

// The schema of transform.init is checked: names, each once, known types.
func TestCheckSchema(t *testing.T) {
	for _, tt := range []struct {
		fields []record.FieldDef
		want   string
	}{
		{[]record.FieldDef{{Name: "a", Type: record.Int}, {Name: "b"}}, ""},
		{[]record.FieldDef{{Name: ""}}, "has no name"},
		{[]record.FieldDef{{Name: "a"}, {Name: "a"}}, `field "a" of the output is listed twice`},
		{[]record.FieldDef{{Name: "a", Type: "money"}}, `field "a" of the output has the unknown type "money"`},
	} {
		err := checkSchema(&record.Schema{Fields: tt.fields})
		if (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)) {
			t.Errorf("%v: %v", tt.fields, err)
		}
	}
	if checkSchema(nil) != nil {
		t.Error("nil")
	}
}
