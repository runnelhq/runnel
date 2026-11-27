package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/secretstore"
	"github.com/runnelhq/runnel/internal/vars"
)

// Codes of error responses of the secret endpoints (S39g1), for the editor.
const (
	// CodeSecretsReadOnly: the editor may not set or delete values of this server.
	CodeSecretsReadOnly = "secrets_read_only"
	// CodeNotLocal: the environment reads the secret from another store than local.
	CodeNotLocal = "not_local"
	// CodeSecretKey: the local store has no usable key (RUNNEL_MASTER_KEY).
	CodeSecretKey = "secret_key"
	// CodeOtherKey: the secrets of the project were stored with another key.
	CodeOtherKey = "other_key"
)

// maxSecretBody limits the body of PUT /secrets/{name}: a value of secretstore.MaxValue
// as JSON, with room for escaped line breaks.
const maxSecretBody = 2*secretstore.MaxValue + 4096

// secretsInfo answers GET /secrets (S39c1, S39g1): every ${secret:…} of the project with
// its uses and, per environment, the store it comes from and whether it is set – never a
// value. The editor shows it at fields for secrets and in the overview of project.yaml
// (mockup S39f).
type secretsInfo struct {
	Environments []string     `json:"environments"`
	Secrets      []secretInfo `json:"secrets"`
	Truncated    bool         `json:"truncated,omitempty"`
	Local        localInfo    `json:"local"`
}

type secretInfo struct {
	Name string `json:"name"`
	// Store is the store named in the reference (${secret:env/NAME}); empty for the
	// default store of each environment.
	Store string        `json:"store,omitempty"`
	Uses  []variableUse `json:"uses"`
	// Environments holds the state by environment; without environments under "".
	Environments map[string]secretState `json:"environments"`
}

type secretState struct {
	Store string `json:"store"`
	// State is set, missing or unavailable (the store cannot be read).
	State string `json:"state"`
	// Updated is when a value of the local store was set (RFC 3339).
	Updated string `json:"updated,omitempty"`
}

// localInfo describes the local secret store this server reads and writes (S39g1).
type localInfo struct {
	// Store is "user" (the store of the account the server runs as) or "service" (the
	// store of the Windows service, %ProgramData%\runnel\secrets).
	Store string `json:"store"`
	// ReadOnly says why the editor cannot set or delete values: "service"; empty: it can.
	ReadOnly string `json:"readOnly,omitempty"`
	// Error says why the store cannot be read, without internal details (those are in
	// the server log).
	Error string `json:"error,omitempty"`
	// Unused are the values in the store that no file of the project reads from the
	// local store; empty when the store cannot be read or the files were truncated.
	Unused []localValue `json:"unused"`
}

type localValue struct {
	Name    string `json:"name"`
	Updated string `json:"updated"`
}

func (s *Server) handleSecrets(w http.ResponseWriter, _ *http.Request) {
	info := secretsInfo{Environments: []string{}, Secrets: []secretInfo{}, Local: localInfo{Store: "user", Unused: []localValue{}}}
	byRef := map[string]*secretInfo{}
	truncated, err := s.walkYAML(func(p string, data []byte) {
		uses, err := model.ScanSecrets(p, data)
		if err != nil {
			return // a broken file has no uses
		}
		for _, u := range uses {
			key := u.Store + "/" + u.Name
			si := byRef[key]
			if si == nil {
				si = &secretInfo{Name: u.Name, Store: u.Store, Uses: []variableUse{}, Environments: map[string]secretState{}}
				byRef[key] = si
			}
			si.Uses = append(si.Uses, variableUse{File: p, Line: u.Pos.Line, Column: u.Pos.Column})
		}
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	info.Truncated = truncated
	local := s.readLocal(&info.Local)
	p, _ := model.LoadProject(s.dir)
	envs := []string{""}
	if p != nil && len(p.Environments) > 0 {
		info.Environments = p.EnvironmentNames()
		envs = info.Environments
	}
	used := map[string]bool{}
	for _, si := range byRef {
		for _, name := range envs {
			var env *model.Environment
			if p != nil {
				env = p.Environments[name]
			}
			si.Environments[name] = localState(p, env, si, local)
			if si.Environments[name].Store == model.SecretStoreLocal {
				used[si.Name] = true
			}
		}
		info.Secrets = append(info.Secrets, *si)
	}
	slices.SortFunc(info.Secrets, func(a, b secretInfo) int {
		return strings.Compare(a.Name+"/"+a.Store, b.Name+"/"+b.Store)
	})
	if local != nil && !truncated {
		for _, v := range local {
			if !used[v.Name] {
				info.Local.Unused = append(info.Local.Unused, localValue{Name: v.Name, Updated: stamp(v.Updated)})
			}
		}
		slices.SortFunc(info.Local.Unused, func(a, b localValue) int { return strings.Compare(a.Name, b.Name) })
	}
	writeJSON(w, http.StatusOK, info)
}

// readLocal lists the values of the project in the local store and fills the description
// of the store; nil if it cannot be read (the details go to the server log once).
func (s *Server) readLocal(info *localInfo) map[string]secretstore.Info {
	store, err := s.secrets()
	var list []secretstore.Info
	if err == nil {
		if store.Machine {
			info.Store = "service"
		}
		info.ReadOnly = secretsReadOnly(store)
		list, err = store.List(s.dir)
	}
	if err != nil {
		info.Error = "the local secret store cannot be read; details are in the server log"
		if errors.Is(err, fs.ErrPermission) {
			info.Error = "access to the file of the local secret store was denied; details are in the server log"
		}
		s.secretMu.Lock()
		if msg := err.Error(); msg != s.secretErr {
			s.secretErr = msg
			s.warn("the local secret store cannot be read: " + msg)
		}
		s.secretMu.Unlock()
		return nil
	}
	m := make(map[string]secretstore.Info, len(list))
	for _, v := range list {
		m[v.Name] = v
	}
	return m
}

// localState is the state of a secret in an environment; values of the local store come
// from local (nil: the store cannot be read).
func localState(p *model.Project, env *model.Environment, si *secretInfo, local map[string]secretstore.Info) secretState {
	store := si.Store
	if store == "" {
		store = model.DefaultSecretStore(p, env)
	}
	if store != model.SecretStoreLocal {
		from, state := model.SecretStatus(p, env, si.Store, si.Name)
		return secretState{Store: from, State: state}
	}
	if p == nil || local == nil {
		return secretState{Store: store, State: model.SecretUnavailable}
	}
	if v, ok := local[si.Name]; ok {
		return secretState{Store: store, State: model.SecretSet, Updated: stamp(v.Updated)}
	}
	return secretState{Store: store, State: model.SecretMissing}
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// secretsReadOnly says why the editor may not change values of store: "service" for the
// store of the Windows service, which only administrators set in a console. S40b adds
// the network mode (off unless --allow-secret-changes).
func secretsReadOnly(store *secretstore.Store) string {
	if store.Machine {
		return "service"
	}
	return ""
}

type setSecretRequest struct {
	// Environment selects the default store of an environment; empty: that of the
	// project (or local without environments).
	Environment string `json:"environment,omitempty"`
	// Store is the store named in the reference (${secret:local/NAME}); it takes
	// precedence over Environment.
	Store string `json:"store,omitempty"`
	Value string `json:"value"`
}

type setSecretResponse struct {
	Name    string `json:"name"`
	Store   string `json:"store"`
	Updated string `json:"updated"`
}

// handleSetSecret stores the value of a secret in the local store of the project
// (S39g1). The value is never logged or returned.
func (s *Server) handleSetSecret(w http.ResponseWriter, r *http.Request) {
	name, store, ok := s.secretFor(w, r)
	if !ok {
		return
	}
	var req setSecretRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSecretBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("the value is larger than %d MB", secretstore.MaxValue>>20))
			return
		}
		if errors.Is(err, io.EOF) {
			err = errors.New("no body")
		}
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	switch {
	case len(req.Value) > secretstore.MaxValue:
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("the value is larger than %d MB", secretstore.MaxValue>>20))
		return
	case req.Value == "":
		writeError(w, http.StatusBadRequest, "the value is empty")
		return
	}
	if err := s.checkLocal(req.Environment, req.Store); err != nil {
		writeErr(w, err)
		return
	}
	now := s.now()
	s.secretMu.Lock()
	err := store.Set(s.dir, name, req.Value, now)
	s.secretMu.Unlock()
	if err != nil {
		s.writeStoreErr(w, "the value was not stored", err)
		return
	}
	s.info(fmt.Sprintf("secret %s set in the local secret store (editor)", name))
	writeJSON(w, http.StatusOK, setSecretResponse{Name: name, Store: model.SecretStoreLocal, Updated: stamp(now)})
}

// handleDeleteSecret removes the value of a secret from the local store of the project.
func (s *Server) handleDeleteSecret(w http.ResponseWriter, r *http.Request) {
	name, store, ok := s.secretFor(w, r)
	if !ok {
		return
	}
	s.secretMu.Lock()
	deleted, err := store.Delete(s.dir, name)
	s.secretMu.Unlock()
	switch {
	case err != nil:
		s.writeStoreErr(w, "the value was not deleted", err)
		return
	case !deleted:
		writeError(w, http.StatusNotFound, fmt.Sprintf("secret %s is not set in the local secret store of this project", name))
		return
	}
	s.info(fmt.Sprintf("secret %s deleted from the local secret store (editor)", name))
	w.WriteHeader(http.StatusNoContent)
}

// secretFor checks the name in the path and that the editor may change the local store.
func (s *Server) secretFor(w http.ResponseWriter, r *http.Request) (string, *secretstore.Store, bool) {
	name := r.PathValue("name")
	if !vars.ValidSecretName(name) {
		writeError(w, http.StatusBadRequest, "invalid secret name (letters, digits, ., - and _)")
		return "", nil, false
	}
	store, err := s.secrets()
	if err != nil {
		s.warn("the local secret store cannot be found: " + err.Error())
		writeError(w, http.StatusInternalServerError, "the local secret store cannot be found; details are in the server log")
		return "", nil, false
	}
	if secretsReadOnly(store) == "service" {
		writeJSON(w, http.StatusForbidden, apiError{Code: CodeSecretsReadOnly,
			Error: "Runnel runs as the Windows service: an administrator sets its secrets in a console with runnel secret set " + name + " --service"})
		return "", nil, false
	}
	return name, store, true
}

// checkLocal checks that a value set for the environment (or the store named in the
// reference) goes to the local store: Runnel only writes there (ADR 0022).
func (s *Server) checkLocal(environment, store string) error {
	if store == "" {
		p, err := model.LoadProject(s.dir)
		if err != nil {
			return errorf(http.StatusUnprocessableEntity, "%s has errors (see Problems)", model.ProjectFile)
		}
		var env *model.Environment
		if environment != "" {
			if env = p.Environments[environment]; env == nil {
				return errorf(http.StatusBadRequest, "unknown environment %q", environment)
			}
		}
		store = model.DefaultSecretStore(p, env)
	}
	if store == model.SecretStoreLocal {
		return nil
	}
	hint := ""
	switch store {
	case model.SecretStoreEnv:
		hint = ": set the environment variable for the account that runs Runnel, then restart Runnel"
	case model.SecretStoreInline:
		hint = ": the value is plain text under secrets: in the file of the environment or project.yaml"
	}
	return &httpError{status: http.StatusConflict, code: CodeNotLocal,
		msg: fmt.Sprintf("the secret comes from the secret store %s, not local; the editor only sets values in the local store%s", store, hint)}
}

// writeStoreErr answers an error of the local store: problems with the key as they are
// (they name no value or path), anything else only in the server log.
func (s *Server) writeStoreErr(w http.ResponseWriter, what string, err error) {
	switch {
	case errors.Is(err, secretstore.ErrNoKey), errors.Is(err, secretstore.ErrInvalidKey):
		writeJSON(w, http.StatusServiceUnavailable, apiError{Code: CodeSecretKey, Error: what + ". " + upperFirst(err.Error()) + ", then restart runnel serve"})
	case errors.Is(err, secretstore.ErrOtherKey):
		writeJSON(w, http.StatusConflict, apiError{Code: CodeOtherKey, Error: what + ": " + err.Error()})
	default:
		s.warn(fmt.Sprintf("%s in the local secret store: %v", what, err))
		writeError(w, http.StatusInternalServerError, what+": the local secret store cannot be written; details are in the server log")
	}
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
