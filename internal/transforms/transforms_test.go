package transforms

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

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/schema"
	"github.com/runnelhq/runnel/internal/transform"
)

// TestContract checks rules that every built-in transform must follow (ADR 0013, 0014),
// like the contract test of the actions.
func TestContract(t *testing.T) {
	r := Builtin()
	if len(r.Types()) == 0 {
		t.Fatal("no built-in transforms")
	}
	for _, typ := range r.Types() {
		def, _ := r.Lookup(typ)
		t.Run(typ, func(t *testing.T) {
			if ns := model.TypeNamespace(typ); !model.IsReservedNamespace(ns) {
				t.Errorf("namespace %q of a built-in transform must be reserved (internal/model/namespaces.go)", ns)
			}
			if !slices.Contains(transform.Categories, def.Category) {
				t.Errorf("category %q is not one of %v", def.Category, transform.Categories)
			}
			if def.Label == "" || !unicode.IsUpper([]rune(def.Label)[0]) {
				t.Errorf("label %q must be a short name starting with a capital letter", def.Label)
			}
			if def.Description == "" || strings.HasSuffix(def.Description, ".") {
				t.Errorf("description %q must be one sentence without a final period", def.Description)
			}
			if _, err := def.New(map[string]any{"no-such-field": true}); err == nil {
				t.Error("unknown field in 'with' was accepted")
			}
			desc, _ := r.Descriptor(typ)
			checkSchema(t, desc.Schema, "")
			if _, err := json.Marshal(desc); err != nil {
				t.Errorf("descriptor cannot be written as JSON: %v", err)
			}
			if _, err := def.New(sample(desc.Schema)); err != nil {
				t.Errorf("configuration built from the schema was rejected (Config and New must use the same struct): %v", err)
			}
		})
	}
}

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

func sample(s *schema.Schema) map[string]any {
	with := map[string]any{}
	for _, p := range s.Properties {
		with[p.Name] = sampleValue(p.Schema)
		// A required list of objects gets one sample entry (e.g. the fields of core.convert).
		if p.Schema.Type == "array" && slices.Contains(s.Required, p.Name) && p.Schema.Items != nil && p.Schema.Items.Type == "object" {
			with[p.Name] = []any{sample(p.Schema.Items)}
		}
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

// TestContractCancellation checks that no built-in transform keeps running after the
// pipeline was cancelled: Process returns nil or the context's error, quickly.
func TestContractCancellation(t *testing.T) {
	r := Builtin()
	for _, typ := range r.Types() {
		def, _ := r.Lookup(typ)
		desc, _ := r.Descriptor(typ)
		tr, err := def.New(sample(desc.Schema))
		if err != nil {
			t.Errorf("%s: %v", typ, err)
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		in := make(chan transform.Batch, 1)
		in <- transform.Batch{record.New(record.Field{Name: "a", Value: 1})}
		close(in)
		out := transform.NewOutput("t", func(record.Record) error { return ctx.Err() }, func(slog.Level, string) {})
		done := make(chan error, 1)
		go func() { done <- tr.Process(ctx, in, out) }()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("%s: cancelled Process returned %v, want nil or context.Canceled", typ, err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("%s: Process did not return after cancellation", typ)
		}
	}
}
