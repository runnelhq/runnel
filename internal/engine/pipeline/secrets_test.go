package pipeline

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/vars"
)

type secretConfig struct {
	Password string `json:"password" doc:"Password" schema:"format=runnel-secret"`
}

// The field for a secret of a transform is resolved with ExpandSecret; the value is
// masked in the log, errors and data preview, but the records keep it (ADR 0022).
func TestPipelineSecrets(t *testing.T) {
	reg := testRegistry(&collected{})
	reg.Register(transform.Definition{Type: "test.secret", Category: transform.CategoryFlow, Label: "Test", Description: "Test",
		Config: secretConfig{},
		New: func(with map[string]any) (transform.Transform, error) {
			pw, _ := with["password"].(string)
			return fn(func(_ context.Context, in <-chan transform.Batch, out *transform.Output) error {
				for range in {
				}
				out.Logf(slog.LevelInfo, "connected with %s", pw)
				return out.Emit(record.New(record.Field{Name: "pw", Value: pw}))
			}), nil
		}})
	p := parse(t, `transforms:
  - { id: gen, type: core.generate-rows, with: { count: 1 } }
  - { id: login, type: test.secret, with: { password: "${secret:pw}" } }
  - { id: end, type: core.dummy }
links:
  - { from: gen, to: login }
  - { from: login, to: end }
`)
	mask := &vars.Mask{}
	scope := vars.NewScope()
	scope.LookupSecret = mask.Lookup(func(_, name string) (string, error) { return "Pa55-" + name, nil })
	var logs []string
	r, err := Prepare(p, Options{Registry: reg, Scope: scope, Mask: mask, SampleRows: 5,
		Observer: func(e Event) {
			if e.Kind == Log {
				logs = append(logs, e.Message)
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	res := r.Run(context.Background())
	if res.Status != Succeeded {
		t.Fatal(res.Err)
	}
	if got := strings.Join(logs, "\n"); got != "connected with ***" {
		t.Errorf("log %q", got)
	}
	if v, _ := res.Samples[1][0].Get("pw"); v != "***" {
		t.Errorf("sample %v", v)
	}

	// Outside a field for a secret, ${secret:…} stops the pipeline before it starts.
	p = parse(t, `transforms:
  - { id: gen, type: core.generate-rows, with: { count: 1, numberField: "${secret:pw}" } }
`)
	if _, err := Prepare(p, Options{Registry: reg, Scope: scope}); err == nil || !strings.Contains(err.Error(), "secrets (${secret:…}) are only allowed in fields for secrets") {
		t.Errorf("secret outside its field: %v", err)
	}
}
