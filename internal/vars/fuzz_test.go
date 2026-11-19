package vars

import "testing"

// FuzzParseExpand checks that arbitrary texts never make Parse or Expand panic and
// that Expand never accepts a text that Parse rejects.
func FuzzParseExpand(f *testing.F) {
	for _, s := range []string{"", "${A}", "$${A}", "${env:X}", "${secret:a.b}", "${", "}${A}${B", "${A:B}", "x${A}y${A}"} {
		f.Add(s)
	}
	scope := NewScope(Layer{Name: "t", Values: map[string]string{"A": "a${B}", "B": "b"}})
	scope.LookupEnv = func(string) (string, bool) { return "env", true }
	f.Fuzz(func(t *testing.T, text string) {
		_, parseErr := Parse(text)
		_, expandErr := scope.Expand(text)
		if parseErr != nil && expandErr == nil {
			t.Errorf("Parse failed (%v) but Expand succeeded for %q", parseErr, text)
		}
	})
}
