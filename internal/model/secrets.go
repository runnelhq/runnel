package model

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/vars"
)

// Secret stores (ADR 0022): ${secret:NAME} reads from the default store of the
// environment (secretStore: in the environment, else in project.yaml, else local);
// ${secret:STORE/NAME} names the store. Built in are local and shared (the local secret
// store of the computer, roadmap S39d/S39e), env (environment variables) and inline
// (secrets: in the environment or project.yaml, plain text; Nachtrag S39a2).
const (
	SecretStoreLocal  = "local"
	SecretStoreShared = "shared"
	SecretStoreEnv    = "env"
	SecretStoreInline = "inline"
)

// SecretStores are the names of the built-in secret stores.
var SecretStores = []string{SecretStoreLocal, SecretStoreShared, SecretStoreEnv, SecretStoreInline}

// DefaultSecretStore returns the store ${secret:NAME} reads from in an environment:
// secretStore of the environment, else of the project, else local. p and env may be nil.
func DefaultSecretStore(p *Project, env *Environment) string {
	switch {
	case env != nil && env.SecretStore != "":
		return env.SecretStore
	case p != nil && p.SecretStore != "":
		return p.SecretStore
	}
	return SecretStoreLocal
}

// SecretLookup returns the function that reads secrets in an environment (for
// vars.Scope.LookupSecret). The error names the store and the secret, never a value.
func SecretLookup(p *Project, env *Environment) func(store, name string) (string, error) {
	def := DefaultSecretStore(p, env)
	return func(store, name string) (string, error) {
		if store == "" {
			store = def
		}
		if err := secretStoreAvailable(store); err != nil {
			return "", fmt.Errorf("secret %q: %w", name, err)
		}
		if store == SecretStoreInline {
			return inlineSecret(p, env, name)
		}
		v, ok := os.LookupEnv(name)
		if !ok || v == "" {
			return "", fmt.Errorf("secret %q: environment variable %s is not set (secret store env)", name, name)
		}
		return v, nil
	}
}

// States of a secret in an environment (SecretStatus).
const (
	SecretSet         = "set"
	SecretMissing     = "missing"
	SecretUnavailable = "unavailable" // the store cannot be read (yet), e.g. local before S39d
)

// SecretStatus tells from which store a secret of an environment comes and whether it is
// set, for the editor (S39c1); it never returns the value. store is the store named in
// the reference, empty for the default store of the environment.
func SecretStatus(p *Project, env *Environment, store, name string) (from, state string) {
	if store == "" {
		store = DefaultSecretStore(p, env)
	}
	if secretStoreAvailable(store) != nil {
		return store, SecretUnavailable
	}
	if store == SecretStoreInline {
		if _, err := inlineSecret(p, env, name); err != nil {
			return store, SecretMissing
		}
		return store, SecretSet
	}
	if v, ok := os.LookupEnv(name); ok && v != "" {
		return store, SecretSet
	}
	return store, SecretMissing
}

// inlineSecret reads a secret from secrets: of the environment, else of project.yaml.
func inlineSecret(p *Project, env *Environment, name string) (string, error) {
	if env != nil {
		if v, ok := env.Secrets[name]; ok {
			return v, nil
		}
	}
	if p != nil {
		if v, ok := p.Secrets[name]; ok {
			return v, nil
		}
	}
	where := ProjectFile
	if env != nil {
		where = EnvironmentsDir + "/" + env.Name + ".yaml or " + ProjectFile
	}
	return "", fmt.Errorf("secret %q is not set under secrets: in %s (secret store inline)", name, where)
}

func checkSecretNames(s *source, secrets map[string]string) Errors {
	var errs Errors
	for _, name := range slices.Sorted(maps.Keys(secrets)) {
		if !vars.ValidSecretName(name) {
			errs = append(errs, s.errorf(keyPath("$.secrets", name), "invalid secret name %q (letters, digits, ., - and _)", name))
		}
	}
	return errs
}

// inlineWarning is the warning for secrets in plain text (secretStore: inline).
const inlineWarning = "secrets in plain text (secretStore: inline): meant for development and simple APIs; keep this file out of shared repositories and use another secret store for production"

// InlineSecretWarnings warns about secrets in plain text in project.yaml and its
// environments (for runnel validate; loading a project does not warn).
func (p *Project) InlineSecretWarnings() Errors {
	var errs Errors
	if p.src != nil && len(p.Secrets) > 0 {
		errs = append(errs, p.src.warnf("$.secrets", inlineWarning))
	}
	for _, name := range p.EnvironmentNames() {
		errs = append(errs, p.Environments[name].InlineSecretWarnings()...)
	}
	return errs
}

// InlineSecretWarnings warns about secrets in plain text in an environment.
func (env *Environment) InlineSecretWarnings() Errors {
	if env == nil || env.src == nil || len(env.Secrets) == 0 {
		return nil
	}
	return Errors{env.src.warnf("$.secrets", inlineWarning)}
}

// secretStoreAvailable reports why a store cannot be read; nil for env and inline.
func secretStoreAvailable(store string) error {
	switch store {
	case SecretStoreEnv, SecretStoreInline:
		return nil
	case SecretStoreLocal, SecretStoreShared:
		return fmt.Errorf("the %s secret store is not available yet; set secretStore: env in the environment or project.yaml, or write ${secret:env/NAME}", store)
	}
	return fmt.Errorf("unknown secret store %q (available: %s)", store, strings.Join(SecretStores, ", "))
}

func checkSecretStore(s *source, store string) Errors {
	if store == "" || slices.Contains(SecretStores, store) {
		return nil
	}
	return Errors{s.errorf("$.secretStore", "unknown secret store %q (expected %s)", store, strings.Join(SecretStores, ", "))}
}

// checkSecrets reports ${secret:…} in the values of a file outside the places allowed
// for secrets (allowed may be nil: nowhere), and secrets in allowed places whose store
// cannot be read (stores are the default stores the file is used with).
func checkSecrets(s *source, allowed func(path string) bool, stores []string) Errors {
	var errs Errors
	for _, sc := range s.scalars() {
		refs, err := vars.Parse(sc.value)
		if err != nil {
			continue // reported by checkReferenceSyntax
		}
		for _, r := range refs {
			if r.Kind != vars.KindSecret {
				continue
			}
			if allowed == nil || !allowed(sc.path) {
				errs = append(errs, &Error{File: s.name, Pos: sc.pos, Path: sc.path,
					Msg: fmt.Sprintf("%s is not allowed here: secrets may only be used in fields for secrets (e.g. a password or token), so that they do not end up in logs or files", r)})
				continue
			}
			for _, store := range secretStoresOf(r, stores) {
				if err := secretStoreAvailable(store); err != nil {
					errs = append(errs, &Error{File: s.name, Pos: sc.pos, Path: sc.path, Msg: fmt.Sprintf("%s: %v", r, err)})
					break
				}
			}
		}
	}
	return errs
}

// secretStoresOf returns the stores a reference reads from: its own, else the default
// stores (none if unknown, then only stores named in references are checked).
func secretStoresOf(r vars.Ref, defaults []string) []string {
	if r.Store != "" {
		return []string{r.Store}
	}
	return defaults
}

// SecretStoresOf returns the default secret stores a file of the project is used with:
// one per environment (or the project's without environments). p may be nil.
func SecretStoresOf(p *Project, only string) []string {
	if p == nil {
		return []string{SecretStoreLocal}
	}
	var out []string
	add := func(env *Environment) {
		if s := DefaultSecretStore(p, env); !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	switch {
	case only != "":
		add(p.Environments[only])
	case len(p.Environments) == 0:
		add(nil)
	default:
		for _, name := range p.EnvironmentNames() {
			add(p.Environments[name])
		}
	}
	return out
}

// CheckSecrets reports ${secret:…} outside the fields for secrets of the actions
// (isSecret tells whether a field of an action type holds a secret), and secrets whose
// store cannot be read with the given default stores (SecretStoresOf).
func (w *Workflow) CheckSecrets(isSecret func(typ, field string) bool, stores []string) Errors {
	types := make([]string, len(w.Actions))
	for i, a := range w.Actions {
		types[i] = a.Type
	}
	return checkSecrets(w.src, stepSecretPaths("actions", types, isSecret), stores)
}

// CheckSecrets is Workflow.CheckSecrets for the transforms of a pipeline.
func (pl *Pipeline) CheckSecrets(isSecret func(typ, field string) bool, stores []string) Errors {
	types := make([]string, len(pl.Transforms))
	for i, t := range pl.Transforms {
		types[i] = t.Type
	}
	return checkSecrets(pl.src, stepSecretPaths("transforms", types, isSecret), stores)
}

// stepSecretPaths allows $.<list>[i].with.<field> for the fields for secrets.
func stepSecretPaths(list string, types []string, isSecret func(typ, field string) bool) func(path string) bool {
	return func(path string) bool {
		var i int
		var field string
		if n, _ := fmt.Sscanf(path, "$."+list+"[%d].with.%s", &i, &field); n != 2 || i < 0 || i >= len(types) || isSecret == nil {
			return false
		}
		return isSecret(types[i], field)
	}
}
