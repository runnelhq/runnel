// Package actiontest contains helpers for testing actions.
package actiontest

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/schema"
)

// Logged is a message an action wrote to the run log.
type Logged struct {
	Level slog.Level
	Msg   string
}

// New creates an action from its definition and fails the test on an error.
func New(t *testing.T, def action.Definition, with map[string]any) action.Action {
	t.Helper()
	a, err := def.New(with)
	if err != nil {
		t.Fatalf("%s: unexpected configuration error: %v", def.Type, err)
	}
	return a
}

// ConfigError expects def.New to fail with an error containing want.
func ConfigError(t *testing.T, def action.Definition, with map[string]any, want string) {
	t.Helper()
	if _, err := def.New(with); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("%s %v: error %v, want containing %q", def.Type, with, err, want)
	}
}

// Execute runs an action with the run id "run" and the action id "act" and
// returns its result, its log messages and its error.
func Execute(ctx context.Context, a action.Action) (action.Result, []Logged, error) {
	var logs []Logged
	ec := &action.ExecContext{RunID: "run", ActionID: "act", Log: func(l slog.Level, m string) {
		logs = append(logs, Logged{l, m})
	}}
	res, err := a.Execute(ctx, ec)
	return res, logs, err
}

// CheckReference checks the reference of a built-in action or transform (roadmap E15b):
// errors for every type, records for every transform and only there, texts as sentences
// with a final period (they may start with the name of a field), output types from types, action outputs named by their path below
// output, and names from the configuration ("<fields[].name>") that exist in its schema.
func CheckReference(t *testing.T, kind string, ref action.Reference, s *schema.Schema, types []string) {
	t.Helper()
	sentence := func(what, text string) {
		t.Helper()
		if strings.TrimSpace(text) == "" || !strings.HasSuffix(text, ".") {
			t.Errorf("%s %q must be English sentences ending with a period", what, text)
		}
	}
	if ref.Errors == "" {
		t.Errorf("missing Errors: when does the %s fail (and what goes to the error link)?", kind)
	} else {
		sentence("Errors", ref.Errors)
	}
	switch {
	case kind == "transform" && ref.Records == "":
		t.Error("missing Records: which records does the transform pass on, over which links?")
	case kind == "transform":
		sentence("Records", ref.Records)
	case ref.Records != "":
		t.Error("Records is for transforms only")
	}
	for _, l := range ref.Limits {
		sentence("Limits", l)
	}
	for _, o := range ref.Outputs {
		if !slices.Contains(types, o.Type) {
			t.Errorf("output %q: type %q is not one of %v", o.Name, o.Type, types)
		}
		if o.Description == "" || strings.HasSuffix(o.Description, ".") {
			t.Errorf("output %q: description %q must be one phrase without a final period", o.Name, o.Description)
		}
		if kind == "action" && o.Name != "output" && !strings.HasPrefix(o.Name, "output.") && !strings.HasPrefix(o.Name, "output[]") {
			t.Errorf("output %q must be named by its path, e.g. output, output.status or output[].path", o.Name)
		}
		for rest := o.Name; strings.Contains(rest, "<"); {
			_, after, _ := strings.Cut(rest, "<")
			path, after, ok := strings.Cut(after, ">")
			if !ok || !hasField(s, path) {
				t.Errorf("output %q: %q is no field of the configuration", o.Name, path)
			}
			rest = after
		}
	}
}

// hasField reports whether a path like "fields[].name" names a field of a schema.
func hasField(s *schema.Schema, path string) bool {
	for part := range strings.SplitSeq(path, ".") {
		name, list := strings.CutSuffix(part, "[]")
		var next *schema.Schema
		for _, p := range s.Properties {
			if p.Name == name {
				next = p.Schema
			}
		}
		if next == nil || list && next.Items == nil {
			return false
		}
		if s = next; list {
			s = next.Items
		}
	}
	return s != nil
}
