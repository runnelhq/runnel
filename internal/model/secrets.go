package model

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/vars"
)

// Secret stores (ADR 0022): ${secret:NAME} reads from the default store of the
// environment (secretStore: in the environment, else in project.yaml, else local);
// ${secret:STORE/NAME} names the store. Built in are local and shared (the local secret
// store of the computer, roadmap S39d/S39e) and env (environment variables).
const (
	SecretStoreLocal  = "local"
	SecretStoreShared = "shared"
	SecretStoreEnv    = "env"
)

// SecretStores are the names of the built-in secret stores.
var SecretStores = []string{SecretStoreLocal, SecretStoreShared, SecretStoreEnv}

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
		v, ok := os.LookupEnv(name) // env is the only store so far
		if !ok || v == "" {
			return "", fmt.Errorf("secret %q: environment variable %s is not set (secret store env)", name, name)
		}
		return v, nil
	}
}

// secretStoreAvailable reports why a store cannot be read; nil for env.
func secretStoreAvailable(store string) error {
	switch store {
	case SecretStoreEnv:
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
