package vars

import (
	"errors"
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
		{"${secret:pw}", "", "secrets are not available"},
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

	s.LookupSecret = func(n string) (string, error) { return "s3cret-" + n, nil }
	if got, err := s.Expand("${secret:pw}"); err != nil || got != "s3cret-pw" {
		t.Errorf("secret: got (%q, %v)", got, err)
	}
}

func TestValidName(t *testing.T) {
	for name, want := range map[string]bool{"A": true, "_x1": true, "IMPORT_DATE": true, "": false, "1A": false, "A-B": false, "Ä": false} {
		if got := ValidName(name); got != want {
			t.Errorf("ValidName(%q) = %v, want %v", name, got, want)
		}
	}
}
