package server

import (
	"net/http"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/model"
)

// secretsInfo answers GET /secrets (S39c1): every ${secret:…} of the project with its
// uses and, per environment, the store it comes from and whether it is set – never a
// value. The editor shows it at fields for secrets (mockup S39b/S39f).
type secretsInfo struct {
	Environments []string     `json:"environments"`
	Secrets      []secretInfo `json:"secrets"`
	Truncated    bool         `json:"truncated,omitempty"`
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
	// State is set, missing or unavailable (the store cannot be read yet).
	State string `json:"state"`
}

func (s *Server) handleSecrets(w http.ResponseWriter, _ *http.Request) {
	info := secretsInfo{Environments: []string{}, Secrets: []secretInfo{}}
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
	p, _ := model.LoadProject(s.dir)
	envs := []string{""}
	if p != nil && len(p.Environments) > 0 {
		info.Environments = p.EnvironmentNames()
		envs = info.Environments
	}
	for _, si := range byRef {
		for _, name := range envs {
			var env *model.Environment
			if p != nil {
				env = p.Environments[name]
			}
			from, state := model.SecretStatus(p, env, si.Store, si.Name)
			si.Environments[name] = secretState{Store: from, State: state}
		}
		info.Secrets = append(info.Secrets, *si)
	}
	slices.SortFunc(info.Secrets, func(a, b secretInfo) int {
		return strings.Compare(a.Name+"/"+a.Store, b.Name+"/"+b.Store)
	})
	writeJSON(w, http.StatusOK, info)
}
