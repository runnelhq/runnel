package connections_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/runs"
)

// TestContract checks the texts of every built-in connection type for the catalog and the
// help (E15e): the example is a connection file that validate accepts without errors, and
// errors and test are sentences. Every built-in type has a test (package conntest).
func TestContract(t *testing.T) {
	r := connections.Builtin()
	for _, typ := range r.Types() {
		t.Run(typ, func(t *testing.T) {
			d, _ := r.Descriptor(typ)
			if d.Label == "" || d.Description == "" || strings.HasSuffix(d.Description, ".") {
				t.Errorf("label %q and description %q: both needed, the description without a final period", d.Label, d.Description)
			}
			for what, text := range map[string]string{"Errors": d.Errors, "Test": d.Test} {
				if strings.TrimSpace(text) == "" || !strings.HasSuffix(text, ".") {
					t.Errorf("%s %q must be English sentences ending with a period", what, text)
				}
			}
			if _, err := json.Marshal(d); err != nil {
				t.Errorf("descriptor cannot be written as JSON: %v", err)
			}
			checkExample(t, typ, d.Example)
		})
	}
}

// warned are the types whose example validate warns about by design: ftp always warns
// that it is unencrypted.
var warned = map[string]bool{connections.TypeFTP: true}

// checkExample reads the example as the file connections/<name>.yaml and checks it like
// validate does: fields, secrets only in fields for secrets, warnings.
func checkExample(t *testing.T, typ, example string) {
	t.Helper()
	if !strings.HasSuffix(example, "\n") {
		t.Fatalf("missing Example (a connection file ending with a line break): %q", example)
	}
	name := "example"
	for line := range strings.SplitSeq(example, "\n") {
		if v, ok := strings.CutPrefix(line, "name: "); ok {
			name = v
		}
	}
	c, err := model.ReadConnection("connections/"+name+".yaml", []byte(example))
	if err != nil {
		t.Fatalf("example is no valid connection file: %v", err)
	}
	if c.Type != typ {
		t.Errorf("example has type %q", c.Type)
	}
	if !strings.Contains(example, "${secret:") && typ != connections.TypeSQLite {
		t.Error("the example should show a secret field with ${secret:NAME}")
	}
	errs := runs.CheckConnection(c, nil, "")
	if errs.HasErrors() || (len(errs) > 0) != warned[typ] {
		t.Errorf("validate reports for the example: %v", errs.Error())
	}
}
