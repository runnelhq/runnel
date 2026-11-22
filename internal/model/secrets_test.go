package model

import (
	"strings"
	"testing"
)

// Secret stores (ADR 0022, roadmap S39a): the default store comes from the environment,
// else project.yaml, else local; only env can be read so far.
func TestSecretStores(t *testing.T) {
	p := &Project{SecretStore: "env"}
	prod := &Environment{Name: "prod", SecretStore: "local"}
	for _, tt := range []struct {
		p    *Project
		env  *Environment
		want string
	}{
		{nil, nil, "local"},
		{&Project{}, nil, "local"},
		{p, nil, "env"},
		{p, &Environment{Name: "dev"}, "env"},
		{p, prod, "local"},
	} {
		if got := DefaultSecretStore(tt.p, tt.env); got != tt.want {
			t.Errorf("DefaultSecretStore(%+v, %+v) = %q, want %q", tt.p, tt.env, got, tt.want)
		}
	}

	t.Setenv("RUNNEL_TEST_SECRET", "s3cret")
	lookup := SecretLookup(p, nil)
	if v, err := lookup("", "RUNNEL_TEST_SECRET"); err != nil || v != "s3cret" {
		t.Errorf("env store: %q, %v", v, err)
	}
	for _, tt := range []struct{ store, name, want string }{
		{"", "RUNNEL_TEST_UNSET", `secret "RUNNEL_TEST_UNSET": environment variable RUNNEL_TEST_UNSET is not set (secret store env)`},
		{"local", "pw", `secret "pw": the local secret store is not available yet; set secretStore: env`},
		{"shared", "pw", "the shared secret store is not available yet"},
		{"vault", "pw", `unknown secret store "vault" (available: local, shared, env, inline)`},
	} {
		if _, err := lookup(tt.store, tt.name); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("lookup(%q, %q): %v, want %q", tt.store, tt.name, err, tt.want)
		}
	}
	// Without secretStore anywhere, ${secret:NAME} reads from local.
	if _, err := SecretLookup(nil, nil)("", "RUNNEL_TEST_SECRET"); err == nil || !strings.Contains(err.Error(), "local secret store is not available yet") {
		t.Errorf("default store: %v", err)
	}
	if v, err := SecretLookup(nil, nil)("env", "RUNNEL_TEST_SECRET"); err != nil || v != "s3cret" {
		t.Errorf("${secret:env/…} without secretStore: %q, %v", v, err)
	}

	envs := map[string]*Environment{"dev": {Name: "dev"}, "prod": prod}
	got := SecretStoresOf(&Project{SecretStore: "env", Environments: envs}, "")
	if strings.Join(got, ",") != "env,local" {
		t.Errorf("SecretStoresOf = %v", got)
	}
	if got := SecretStoresOf(&Project{SecretStore: "env", Environments: envs}, "dev"); strings.Join(got, ",") != "env" {
		t.Errorf("SecretStoresOf(dev) = %v", got)
	}
}

// project.yaml and environments: secretStore must name a store; their values must not
// hold secrets, since variables end up in logs and files.
func TestSecretsInProjectFiles(t *testing.T) {
	_, err := ReadProject("project.yaml", []byte("kind: Project\napiVersion: runnel/v1\nname: p\nsecretStore: vault\nvariables:\n  PW: ${secret:pw}\n"), nil)
	for _, want := range []string{
		`project.yaml:4:14: unknown secret store "vault" (expected local, shared, env, inline)`,
		`project.yaml:6:7: ${secret:pw} is not allowed here: secrets may only be used in fields for secrets`,
	} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("project: %v\nwant %s", err, want)
		}
	}
	env, err := ReadEnvironment("environments/prod.yaml", []byte("kind: Environment\napiVersion: runnel/v1\nname: prod\nsecretStore: env\n"))
	if err != nil || env.SecretStore != "env" {
		t.Errorf("environment: %+v, %v", env, err)
	}
	_, err = ReadEnvironment("environments/prod.yaml", []byte("kind: Environment\napiVersion: runnel/v1\nname: prod\nvariables:\n  TOKEN: x${secret:env/T}\n"))
	if err == nil || !strings.Contains(err.Error(), "${secret:env/T} is not allowed here") {
		t.Errorf("environment variable with a secret: %v", err)
	}
}

const secretWorkflow = `kind: Workflow
apiVersion: runnel/v1
name: secrets
actions:
  - id: start
    type: core.start
  - id: login
    type: test.login
    with:
      user: admin ${secret:wrong}
      password: ${secret:pw}
  - id: other
    type: test.login
    with:
      password: ${secret:vault/pw}
links:
  - { from: start, to: login }
  - { from: login, to: other }
`

// Workflows: ${secret:…} only in fields for secrets, and only from stores that can be read.
func TestWorkflowCheckSecrets(t *testing.T) {
	w, err := ReadWorkflow("w.workflow.yaml", []byte(secretWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	isSecret := func(typ, field string) bool { return typ == "test.login" && field == "password" }
	errs := w.CheckSecrets(isSecret, []string{"env"})
	want := []string{
		"w.workflow.yaml:10:13: ${secret:wrong} is not allowed here",
		`w.workflow.yaml:15:17: ${secret:vault/pw}: unknown secret store "vault"`,
	}
	if len(errs) != len(want) {
		t.Fatalf("errors:\n%v", errs)
	}
	for i, e := range errs {
		if !strings.Contains(e.Error(), want[i]) {
			t.Errorf("error %d = %v, want %q", i, e, want[i])
		}
	}
	// With local as the default store of an environment, the field cannot be read yet.
	errs = w.CheckSecrets(isSecret, []string{"env", "local"})
	if !strings.Contains(errs.Error(), "w.workflow.yaml:11:17: ${secret:pw}: the local secret store is not available yet") {
		t.Errorf("local: %v", errs)
	}
	// Without known stores (no project) only stores named in the reference are checked.
	if errs := w.CheckSecrets(isSecret, nil); len(errs) != 2 {
		t.Errorf("no stores: %v", errs)
	}
}

// http triggers take their token from a secret as well as from an environment variable;
// secrets elsewhere in the trigger are errors.
func TestTriggerTokenSecret(t *testing.T) {
	src := strings.Replace(httpSource, `"${env:RUNNEL_TEST_TOKEN}"`, `"${secret:env/RUNNEL_TEST_TOKEN}"`, 1)
	tr, err := ParseTrigger("triggers/customer.yaml", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.HasToken() || tr.TokenVariable() != "" {
		t.Errorf("HasToken %v, TokenVariable %q", tr.HasToken(), tr.TokenVariable())
	}
	if _, err := tr.Token(); err == nil || !strings.Contains(err.Error(), "secrets are not available") {
		t.Errorf("without UseSecrets: %v", err)
	}
	tr.UseSecrets(SecretLookup(nil, nil))
	t.Setenv("RUNNEL_TEST_TOKEN", "t0ken-value")
	if tok, err := tr.Token(); err != nil || tok != "t0ken-value" {
		t.Errorf("token %q, %v", tok, err)
	}
	t.Setenv("RUNNEL_TEST_TOKEN", "")
	if _, err := tr.Token(); err == nil || !strings.Contains(err.Error(), "environment variable RUNNEL_TEST_TOKEN is not set") {
		t.Errorf("unset: %v", err)
	}

	for text, want := range map[string]string{
		strings.Replace(httpSource, `"${env:RUNNEL_TEST_TOKEN}"`, `"Bearer ${secret:tok}"`, 1):              "the token must not be written into the file; write token: ${secret:NAME}",
		strings.Replace(httpSource, "maxConcurrent: 3", "maxConcurrent: 3\nparams:\n  PW: ${secret:pw}", 1): "${secret:pw} is not allowed here",
	} {
		if _, err := ParseTrigger("triggers/x.yaml", []byte(text)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v\nwant %q", err, want)
		}
	}
}

// secretStore: inline reads secrets: of the environment, else of project.yaml; plain
// text, so validate warns (Nachtrag S39a2).
func TestInlineSecrets(t *testing.T) {
	p, err := ReadProject("project.yaml", []byte("kind: Project\napiVersion: runnel/v1\nname: p\nsecretStore: inline\nsecrets:\n  api-token: proj-123\n  shared.key: proj-key\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := ReadEnvironment("environments/dev.yaml", []byte("kind: Environment\napiVersion: runnel/v1\nname: dev\nsecrets:\n  api-token: dev-123\n"))
	if err != nil {
		t.Fatal(err)
	}
	p.Environments = map[string]*Environment{"dev": dev}
	lookup := SecretLookup(p, dev)
	for name, want := range map[string]string{"api-token": "dev-123", "shared.key": "proj-key"} {
		if v, err := lookup("", name); err != nil || v != want {
			t.Errorf("%s = %q, %v", name, v, err)
		}
	}
	if v, err := SecretLookup(p, nil)("inline", "api-token"); err != nil || v != "proj-123" {
		t.Errorf("project only: %q, %v", v, err)
	}
	if _, err := lookup("", "missing"); err == nil || err.Error() != `secret "missing" is not set under secrets: in environments/dev.yaml or project.yaml (secret store inline)` {
		t.Errorf("missing: %v", err)
	}

	warnings := p.InlineSecretWarnings()
	if len(warnings) != 2 || warnings[0].Severity != SeverityWarning ||
		!strings.Contains(warnings.Error(), "project.yaml:6:3: warning: secrets in plain text (secretStore: inline)") ||
		!strings.Contains(warnings.Error(), "environments/dev.yaml:5:3: warning: secrets in plain text") {
		t.Errorf("warnings: %v", warnings)
	}
	if (&Project{}).InlineSecretWarnings() != nil {
		t.Error("no secrets, no warning")
	}

	_, err = ReadEnvironment("environments/dev.yaml", []byte("kind: Environment\napiVersion: runnel/v1\nname: dev\nsecrets:\n  \"bad name\": x\n"))
	if err == nil || !strings.Contains(err.Error(), `invalid secret name "bad name"`) {
		t.Errorf("bad name: %v", err)
	}
}
