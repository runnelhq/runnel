package vars

import (
	"errors"
	"fmt"
	"testing"
)

func TestMask(t *testing.T) {
	var m Mask
	m.Add("s3cret-token")
	m.Add("s3cret") // contained in the longer one: the longer one is replaced whole
	m.Add("abc")    // under 4 characters: not masked
	m.Add("s3cret") // twice is fine
	if got := m.String("token s3cret-token, pw s3cret, abc"); got != "token ***, pw ***, abc" {
		t.Errorf("String = %q", got)
	}

	base := errors.New("login with s3cret failed")
	err := m.Error(fmt.Errorf("action: %w", base))
	if err.Error() != "action: login with *** failed" || !errors.Is(err, base) {
		t.Errorf("Error = %v (Is %v)", err, errors.Is(err, base))
	}
	if plain := errors.New("no secret"); m.Error(plain) != plain { //nolint:errorlint // the same error value is expected
		t.Error("an error without a secret is passed on as it is")
	}
	if m.Error(nil) != nil {
		t.Error("nil error")
	}

	in := map[string]any{"token": "s3cret-token", "list": []any{"x s3cret", 42}, "n": 1.5}
	out := m.Map(in)
	if out["token"] != "***" || out["list"].([]any)[0] != "x ***" || out["list"].([]any)[1] != 42 || out["n"] != 1.5 {
		t.Errorf("Map = %v", out)
	}
	if in["token"] != "s3cret-token" {
		t.Error("Map changed its input")
	}

	var none *Mask // a run without secrets
	if none.String("s3cret") != "s3cret" || none.Map(in)["token"] != "s3cret-token" {
		t.Error("nil mask must not change anything")
	}
	none.Add("x") // no panic
}

func TestMaskLookup(t *testing.T) {
	var m Mask
	lookup := m.Lookup(func(_, name string) (string, error) {
		if name == "missing" {
			return "", errors.New("not set")
		}
		return "value-of-" + name, nil
	})
	s := NewScope()
	s.LookupSecret = lookup
	if _, err := s.ExpandSecret("${secret:db}"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExpandSecret("${secret:missing}"); err == nil {
		t.Fatal("missing secret")
	}
	if got := m.String("connect with value-of-db"); got != "connect with ***" {
		t.Errorf("String = %q", got)
	}
}
