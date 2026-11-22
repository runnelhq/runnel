package actions

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/schema"
)

// TestContract checks rules that every built-in action must follow (ADR 0013, 0014).
// It runs automatically for every action added to a namespace.
func TestContract(t *testing.T) {
	r := Builtin()
	if len(r.Types()) == 0 {
		t.Fatal("no built-in actions")
	}
	for _, typ := range r.Types() {
		def, _ := r.Lookup(typ)
		t.Run(typ, func(t *testing.T) {
			if !model.ValidTypeName(typ) {
				t.Errorf("invalid type name")
			}
			if ns := model.TypeNamespace(typ); !model.IsReservedNamespace(ns) {
				t.Errorf("namespace %q of a built-in action must be reserved (internal/model/namespaces.go)", ns)
			}
			checkTexts(t, def)
			if def.New == nil {
				t.Fatal("missing constructor")
			}
			// Unknown fields must be rejected, so that typos in workflows are reported.
			if _, err := def.New(map[string]any{"no-such-field": true}); err == nil {
				t.Error("unknown field in 'with' was accepted")
			}
			desc, _ := r.Descriptor(typ)
			checkSchema(t, desc.Schema, "")
			if _, err := json.Marshal(desc); err != nil {
				t.Errorf("descriptor cannot be written as JSON: %v", err)
			}
			// Every field of the schema must be accepted by New: Config and New use the same struct.
			if _, err := def.New(sample(desc.Schema)); err != nil {
				t.Errorf("configuration built from the schema was rejected (Config and New must use the same struct): %v", err)
			}
			checkExample(t, def, desc.Schema)
		})
	}
}

// checkTexts checks the texts shown in the editor's palette.
func checkTexts(t *testing.T, def action.Definition) {
	t.Helper()
	if !slices.Contains(action.Categories, def.Category) {
		t.Errorf("category %q is not one of %v", def.Category, action.Categories)
	}
	if def.Label == "" || !unicode.IsUpper([]rune(def.Label)[0]) {
		t.Errorf("label %q must be a short name starting with a capital letter", def.Label)
	}
	if def.Description == "" || strings.HasSuffix(def.Description, ".") {
		t.Errorf("description %q must be one sentence without a final period", def.Description)
	}
}

// checkSchema requires a description for every field, so that editor and tools can explain it.
func checkSchema(t *testing.T, s *schema.Schema, path string) {
	t.Helper()
	for _, p := range s.Properties {
		name := strings.TrimPrefix(path+"."+p.Name, ".")
		if p.Schema.Description == "" {
			t.Errorf("field %q: missing description (tag doc:\"…\")", name)
		}
		checkSchema(t, p.Schema, name)
	}
	if s.Items != nil {
		checkSchema(t, s.Items, path+"[]")
	}
}

// sample builds a valid configuration from a schema: every field gets its first
// example, its default, its first allowed value or a value of its type.
func sample(s *schema.Schema) map[string]any {
	with := map[string]any{}
	for _, p := range s.Properties {
		with[p.Name] = sampleValue(p.Schema)
	}
	return with
}

func sampleValue(s *schema.Schema) any {
	switch {
	case len(s.Examples) > 0:
		return s.Examples[0]
	case s.Default != nil:
		return s.Default
	case len(s.Enum) > 0:
		return s.Enum[0]
	}
	switch s.Type {
	case "string":
		return "x"
	case "integer", "number":
		return 1
	case "boolean":
		return true
	case "array":
		return []any{}
	case "object":
		return sample(s)
	}
	return nil
}

// TestContractCancellation checks that no built-in action keeps running after the
// run was cancelled. Each action runs with a configuration built from its schema
// (examples, defaults); give required fields a realistic example (schema:"example=…").
func TestContractCancellation(t *testing.T) {
	r := Builtin()
	for _, typ := range r.Types() {
		def, _ := r.Lookup(typ)
		desc, _ := r.Descriptor(typ)
		a, err := def.New(sample(desc.Schema))
		if err != nil {
			t.Errorf("%s: configuration built from the schema was rejected: %v", typ, err)
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		done := make(chan error, 1)
		go func() {
			_, err := a.Execute(ctx, &action.ExecContext{RunID: "run", ActionID: "act", Log: func(slog.Level, string) {}})
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("%s: cancelled run returned %v, want nil or context.Canceled", typ, err)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("%s: still running 2s after the run was cancelled", typ)
		}
	}
}

// checkExample: every action with a configuration has an example for the help of the
// editor, read as in a workflow file, and New accepts it.
func checkExample(t *testing.T, def action.Definition, s *schema.Schema) {
	t.Helper()
	if def.Example == "" {
		if len(s.Properties) > 0 {
			t.Error("missing Example for the help of the editor")
		}
		return
	}
	indented := "      " + strings.ReplaceAll(strings.TrimSuffix(def.Example, "\n"), "\n", "\n      ")
	src := "kind: Workflow\napiVersion: runnel/v1\nname: example\nactions:\n  - id: a\n    type: " + def.Type + "\n    with:\n" + indented + "\n"
	w, err := model.ParseWorkflow("example.workflow.yaml", []byte(src))
	if err != nil {
		t.Fatalf("example is not valid YAML for with: %v", err)
	}
	if _, err := def.New(w.Actions[0].With); err != nil {
		t.Errorf("example was rejected: %v", err)
	}
}
