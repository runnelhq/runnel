package transform

import (
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/record"
)

func TestInitContextUse(t *testing.T) {
	closed := record.Schema{Fields: []record.FieldDef{{Name: "status", Type: record.String}, {Name: "amount", Type: record.Decimal}}}
	ic := NewInitContext(closed)
	ic.Use("status", "fields[0]")
	ic.Use("stauts", "fields[1]")
	ic.Use("_rowNumber", "fields[2]") // metadata is not checked
	p := ic.Problems()
	if len(p) != 1 || p[0].Warning || p[0].Path != "fields[1]" ||
		p[0].Message != `field "stauts" does not exist in the incoming records (did you mean "status"?) (fields: status, amount)` {
		t.Fatalf("closed: %+v", p)
	}

	ic = NewInitContext(closed.With("x", record.Int))
	ic.Use("y", "f")
	if p := ic.Problems(); len(p) != 1 || !strings.Contains(p[0].Message, "(did you mean \"x\"?)") {
		t.Errorf("suggestion: %+v", p)
	}

	open := record.OpenSchema().With("amount", record.Decimal)
	ic = NewInitContext(open)
	ic.Use("amount", "fields[0]")
	ic.Use("amout", "fields[1]")
	if p := ic.Problems(); len(p) != 1 || !p[0].Warning ||
		p[0].Message != `field "amout" is not known in the incoming records (did you mean "amount"?); it is checked when the pipeline runs` {
		t.Errorf("open: %+v", p)
	}
	// Nothing known at all: nothing to compare with, no warning.
	ic = NewInitContext(record.OpenSchema())
	ic.Use("anything", "fields[0]")
	if p := ic.Problems(); len(p) != 0 {
		t.Errorf("unknown schema: %+v", p)
	}

	ic = NewInitContext(record.Schema{})
	ic.Use("a", "")
	if p := ic.Problems(); len(p) != 1 || !strings.Contains(p[0].Message, "(fields: none)") {
		t.Errorf("empty: %+v", p)
	}
}
