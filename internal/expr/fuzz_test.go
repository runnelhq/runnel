package expr

import "testing"

// FuzzCompile checks that any text compiles or fails cleanly, and that a compiled
// template evaluates without a panic.
func FuzzCompile(f *testing.F) {
	for _, s := range []string{
		"plain", "{{ 1 + 2 }}", "a {{ result.rows }} b {{ run.id }}", "{{ formatDate(today(), 'yyyy-MM-dd') }}",
		"{{ padLeft(params.X, 5, '0') }}", "{{", "}}", "{{ {a: 1} }}", "{{ coalesce(nil, '') }}",
	} {
		f.Add(s)
	}
	env := map[string]any{"params": map[string]any{"X": "7"}, "result": map[string]any{"rows": 3}, "run": map[string]any{"id": "r"}}
	f.Fuzz(func(_ *testing.T, s string) {
		tpl, err := Compile(s, WorkflowRoots)
		if err != nil {
			return
		}
		_, _ = tpl.Eval(env)
	})
}
