package vars

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		text    string
		want    []string
		wantErr string
	}{
		{"no refs", nil, ""},
		{"${A}", []string{"${A}"}, ""},
		{`${INBOX}\customers_${IMPORT_DATE}.csv`, []string{"${INBOX}", "${IMPORT_DATE}"}, ""},
		{"${env:COMPUTERNAME} ${env:ProgramFiles(x86)}", []string{"${env:COMPUTERNAME}", "${env:ProgramFiles(x86)}"}, ""},
		{"${secret:erp-sql.password}", []string{"${secret:erp-sql.password}"}, ""},
		{"price $${NOT_A_VAR} and ${REAL}", []string{"${REAL}"}, ""},
		{"{{ now() }} costs $5", nil, ""},
		{"${A", nil, `unclosed "${"`},
		{"${}", nil, "invalid variable name"},
		{"${1A}", nil, "invalid variable name"},
		{"${A-B}", nil, "invalid variable name"},
		{"${env:}", nil, "invalid environment variable name"},
		{"${secret:a b}", nil, "invalid secret name"},
		{"${foo:bar}", nil, `unknown reference type "foo"`},
	}
	for _, tt := range tests {
		refs, err := Parse(tt.text)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Parse(%q) error = %v, want containing %q", tt.text, err, tt.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q) unexpected error: %v", tt.text, err)
			continue
		}
		var got []string
		for _, r := range refs {
			got = append(got, r.String())
		}
		if strings.Join(got, "|") != strings.Join(tt.want, "|") {
			t.Errorf("Parse(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestScopePrecedence(t *testing.T) {
	// Each layer sets its own name as value for all variables it defines.
	layer := func(name string, vars ...string) Layer {
		values := map[string]string{}
		for _, v := range vars {
			values[v] = name
		}
		return Layer{Name: name, Values: values}
	}
	s := NewScope(
		layer("defaults", "A", "B", "C", "D"),
		layer("project", "B", "C", "D"),
		layer("environment", "C", "D"),
		layer("parameters", "D"),
	)
	for name, want := range map[string]string{"A": "defaults", "B": "project", "C": "environment", "D": "parameters"} {
		if v, layer, ok := s.Lookup(name); !ok || v != want || layer != want {
			t.Errorf("Lookup(%q) = (%q, %q, %v), want %q", name, v, layer, ok, want)
		}
	}
	if s.Has("E") {
		t.Error("Has(E) = true for undefined variable")
	}
}

func TestExpand(t *testing.T) {
	env := map[string]string{"COMPUTERNAME": "SRV01"}
	s := NewScope(
		Layer{Name: "project", Values: map[string]string{
			"BASE":    `D:\data`,
			"INBOX":   `${BASE}\inbox`,
			"HOST":    "${env:COMPUTERNAME}",
			"LOOP_A":  "${LOOP_B}",
			"LOOP_B":  "${LOOP_A}",
			"MISSING": "${NOPE}",
		}},
	)
	s.LookupEnv = func(n string) (string, bool) { v, ok := env[n]; return v, ok }

	tests := []struct {
		text, want, wantErr string
	}{
		{"plain", "plain", ""},
		{`${INBOX}\a.csv`, `D:\data\inbox\a.csv`, ""},
		{"on ${HOST}", "on SRV01", ""},
		{"$${INBOX} stays", "${INBOX} stays", ""},
		{"{{ now() }}", "{{ now() }}", ""},
		{"${LOOP_A}", "", "variable cycle: LOOP_A → LOOP_B → LOOP_A"},
		{"${MISSING}", "", `variable "NOPE" is undefined`},
		{"${env:NOPE}", "", `environment variable "NOPE" is undefined`},
		{"${secret:pw}", "", "${secret:pw}: secrets (${secret:…}) are only allowed in fields for secrets"},
	}
	for _, tt := range tests {
		got, err := s.Expand(tt.text)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Expand(%q) error = %v, want containing %q", tt.text, err, tt.wantErr)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("Expand(%q) = (%q, %v), want %q", tt.text, got, err, tt.want)
		}
	}

	_, err := s.Expand("${UNKNOWN}")
	if !errors.Is(err, ErrUndefined) {
		t.Errorf("undefined variable: error %v does not wrap ErrUndefined", err)
	}

	if _, err := s.ExpandSecret("${secret:pw}"); err == nil || !strings.Contains(err.Error(), "secrets are not available") {
		t.Errorf("secret without store: %v", err)
	}
}

// Secrets (ADR 0022): resolved only by ExpandSecret and only when written into the
// text itself, never through a variable; the store is optional.
func TestExpandSecret(t *testing.T) {
	s := NewScope(Layer{Name: "project", Values: map[string]string{"USER": "runnel", "PW": "${secret:pw}"}})
	s.LookupSecret = func(store, name string) (string, error) {
		if name == "missing" {
			return "", fmt.Errorf("secret %q is not set", name)
		}
		return "[" + store + "]" + name, nil
	}
	for text, want := range map[string]string{
		"${secret:pw}":              "[]pw",
		"${secret:env/API_TOKEN}":   "[env]API_TOKEN",
		"${USER}:${secret:db.pass}": "runnel:[]db.pass",
		"$${secret:pw}":             "${secret:pw}",
	} {
		if got, err := s.ExpandSecret(text); err != nil || got != want {
			t.Errorf("ExpandSecret(%q) = (%q, %v), want %q", text, got, err, want)
		}
	}
	for text, want := range map[string]string{
		"${PW}":             `variable "PW" contains a secret; write ${secret:pw} directly into the field for the secret`,
		"${secret:missing}": `secret "missing" is not set`,
	} {
		if _, err := s.ExpandSecret(text); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ExpandSecret(%q): %v, want %q", text, err, want)
		}
	}
	for _, text := range []string{"${secret:pw}", "${PW}", "x ${secret:env/T}"} {
		if _, err := s.Expand(text); !errors.Is(err, ErrSecretNotAllowed) {
			t.Errorf("Expand(%q): %v, want ErrSecretNotAllowed", text, err)
		}
	}
}

func TestParseSecretStore(t *testing.T) {
	refs, err := Parse("${secret:vault/db-pass} ${secret:pw}")
	if err != nil || len(refs) != 2 || refs[0].Store != "vault" || refs[0].Name != "db-pass" || refs[1].Store != "" || refs[0].String() != "${secret:vault/db-pass}" {
		t.Fatalf("%+v, %v", refs, err)
	}
	for _, bad := range []string{"${secret:a/b/c}", "${secret:/pw}", "${secret:my-store/pw}", "${secret:env/}"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q): no error", bad)
		}
	}
}

func TestValidName(t *testing.T) {
	for name, want := range map[string]bool{"A": true, "_x1": true, "IMPORT_DATE": true, "": false, "1A": false, "A-B": false, "Ä": false} {
		if got := ValidName(name); got != want {
			t.Errorf("ValidName(%q) = %v, want %v", name, got, want)
		}
	}
}
