package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/plugin"
	"github.com/runnelhq/runnel/internal/plugintrust"
	"github.com/runnelhq/runnel/internal/winsvc"
)

// States of a plugin for the editor (mockup S52).
const (
	pluginTrusted    = "trusted"
	pluginNotTrusted = "not-trusted"
	pluginChanged    = "changed"
	pluginError      = "error"
)

// pluginInfo describes a plugin of the project for the editor (S52a): its file, the
// state of its approval and – once trusted – its types; where the project uses it.
type pluginInfo struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Namespace   string   `json:"namespace"`
	Description string   `json:"description,omitempty"`
	Folder      string   `json:"folder"`
	Program     string   `json:"program,omitempty"` // for this computer; empty: none
	Executable  any      `json:"executable"`
	Args        []string `json:"args,omitempty"`
	Files       int      `json:"files"`
	Bytes       int64    `json:"bytes"`
	// Hash is the SHA-256 of the folder now: what "trust" approves.
	Hash string `json:"hash,omitempty"`
	// State is trusted, not-trusted, changed (since trusted) or error (cannot be
	// checked or started); Message says why for error.
	State   string        `json:"state"`
	Message string        `json:"message,omitempty"`
	User    *approvalInfo `json:"user,omitempty"`
	Service *approvalInfo `json:"service,omitempty"`
	// Types are those of describe, only for a trusted plugin that started.
	Types *pluginTypes `json:"types,omitempty"`
	Uses  []pluginUse  `json:"uses"`
}

// approvalInfo is a stored approval; Current is false if the plugin changed since.
type approvalInfo struct {
	Version string    `json:"version,omitempty"`
	Hash    string    `json:"hash"`
	Trusted time.Time `json:"trusted"`
	Current bool      `json:"current"`
}

type pluginTypes struct {
	Actions         []pluginType `json:"actions"`
	Transforms      []pluginType `json:"transforms"`
	ConnectionTypes []pluginType `json:"connectionTypes"`
}

type pluginType struct {
	Type        string `json:"type"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// pluginUse is a step or connection of the project with a type of the plugin, found in
// the files without starting the plugin.
type pluginUse struct {
	File string `json:"file"`
	Kind string `json:"kind"` // action, transform or connection
	ID   string `json:"id"`   // the id of the step or the name of the connection
	Type string `json:"type"`
}

// pluginsResponse answers GET /plugins.
type pluginsResponse struct {
	Plugins []pluginInfo `json:"plugins"`
	// ServiceApprovals is set where approvals for the Windows service exist (Windows).
	ServiceApprovals bool `json:"serviceApprovals"`
	// CanTrust is false where the editor cannot trust plugins, e.g. when Runnel runs as
	// the Windows service (an administrator trusts them with runnel plugins trust --service).
	CanTrust bool `json:"canTrust"`
}

// handlePlugins lists the plugins of the project (S52a). Trusted plugins are started
// for describe unless their types are cached; others are not started.
func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request) {
	store, err := plugin.DefaultStore()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the approvals of plugins cannot be read")
		return
	}
	plugins, _ := model.LoadPlugins(s.dir) // problems of the files are reported by validate
	uses := s.pluginUses()
	res := pluginsResponse{Plugins: []pluginInfo{}, ServiceApprovals: store.Service != "", CanTrust: canTrust()}
	for _, name := range slices.Sorted(maps.Keys(plugins)) {
		res.Plugins = append(res.Plugins, s.pluginInfo(r, plugins[name], store, uses))
	}
	writeJSON(w, http.StatusOK, res)
}

// canTrust reports whether this server may write approvals: not as the Windows service,
// whose approvals only administrators change.
func canTrust() bool {
	service, err := winsvc.IsService()
	return err == nil && !service
}

func (s *Server) pluginInfo(r *http.Request, p *model.Plugin, store plugintrust.Store, uses []pluginUse) pluginInfo {
	info := pluginInfo{Name: p.Name, Version: p.Version, Namespace: p.Namespace, Description: p.Description,
		Folder: path.Join(model.PluginsDir, p.Name), Executable: p.Executable, Args: p.Args, Uses: []pluginUse{}}
	info.Program, _ = p.ExecutableFor("")
	info.Files, info.Bytes, _ = plugintrust.Size(p.Dir)
	for _, u := range uses {
		if u.Type == p.Namespace || strings.HasPrefix(u.Type, p.Namespace+".") || strings.HasPrefix(u.Type, p.Namespace+"-") {
			info.Uses = append(info.Uses, u)
		}
	}
	st, err := plugin.Check(s.dir, p, store)
	if err != nil {
		info.State, info.Message = pluginError, "the plugin cannot be checked: "+err.Error()
		return info
	}
	info.Hash = st.Hash
	user, service, err := store.Approvals(s.dir, p.Name)
	if err == nil {
		info.User, info.Service = approval(user, st.Hash), approval(service, st.Hash)
	}
	switch st.State {
	case plugintrust.NotTrusted:
		info.State = pluginNotTrusted
		return info
	case plugintrust.Changed:
		info.State = pluginChanged
		return info
	}
	d, hint := plugin.Types(r.Context(), s.dir, p, store)
	if hint != "" {
		info.State, info.Message = pluginError, hint
		return info
	}
	info.State = pluginTrusted
	info.Types = &pluginTypes{Actions: []pluginType{}, Transforms: []pluginType{}, ConnectionTypes: []pluginType{}}
	for _, a := range d.Actions {
		info.Types.Actions = append(info.Types.Actions, pluginType{Type: a.Type, Label: a.Label, Description: a.Description})
	}
	for _, t := range d.Transforms {
		info.Types.Transforms = append(info.Types.Transforms, pluginType{Type: t.Type, Label: t.Label, Description: t.Description})
	}
	for _, c := range d.Connections {
		info.Types.ConnectionTypes = append(info.Types.ConnectionTypes, pluginType{Type: c.Type, Label: c.Label, Description: c.Description})
	}
	return info
}

func approval(e *plugintrust.Entry, hash string) *approvalInfo {
	if e == nil {
		return nil
	}
	return &approvalInfo{Version: e.Version, Hash: e.Hash, Trusted: e.Trusted, Current: e.Hash == hash}
}

// pluginUses returns the steps and connections of the project by their types, read from
// the files without starting any plugin.
func (s *Server) pluginUses() []pluginUse {
	var uses []pluginUse
	_, _ = s.walkYAML(func(p string, data []byte) {
		var doc struct {
			Kind    string `yaml:"kind"`
			Name    string `yaml:"name"`
			Type    string `yaml:"type"`
			Actions []struct {
				ID   string `yaml:"id"`
				Type string `yaml:"type"`
			} `yaml:"actions"`
			Transforms []struct {
				ID   string `yaml:"id"`
				Type string `yaml:"type"`
			} `yaml:"transforms"`
		}
		if yaml.Unmarshal(data, &doc) != nil {
			return
		}
		switch doc.Kind {
		case model.KindWorkflow:
			for _, a := range doc.Actions {
				uses = append(uses, pluginUse{File: p, Kind: "action", ID: a.ID, Type: a.Type})
			}
		case model.KindPipeline:
			for _, t := range doc.Transforms {
				uses = append(uses, pluginUse{File: p, Kind: "transform", ID: t.ID, Type: t.Type})
			}
		case model.KindConnection:
			uses = append(uses, pluginUse{File: p, Kind: "connection", ID: doc.Name, Type: doc.Type})
		}
	})
	return uses
}

// trustRequest names the hash the user saw: the approval is only written if the plugin
// is still the same (a plugin that changed in between must be checked again).
type trustRequest struct {
	Hash string `json:"hash"`
}

// handleTrustPlugin trusts a plugin for the user that runs the server, like
// "runnel plugins trust" (S52a); the response is the plugin with its new state.
func (s *Server) handleTrustPlugin(w http.ResponseWriter, r *http.Request) {
	p, store, ok := s.pluginFor(w, r, true)
	if !ok {
		return
	}
	var req trustRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.Hash == "" {
		writeError(w, http.StatusBadRequest, "hash is missing: trust the plugin as shown")
		return
	}
	st, err := store.Check(s.dir, p.Name, p.Dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "the plugin cannot be checked: "+err.Error())
		return
	}
	if st.Hash != req.Hash {
		writeError(w, http.StatusConflict, fmt.Sprintf("plugin %s changed since it was shown; check it again", p.Name))
		return
	}
	if err := store.Trust(s.dir, p.Name, p.Version, st.Hash, false, time.Now()); err != nil {
		writeError(w, http.StatusInternalServerError, "the approval cannot be saved")
		s.warn(fmt.Sprintf("trust plugin %s: %v", p.Name, err))
		return
	}
	s.info(fmt.Sprintf("plugin %s %s trusted (SHA-256 %s)", p.Name, p.Version, st.Hash))
	writeJSON(w, http.StatusOK, s.pluginInfo(r, p, store, s.pluginUses()))
}

// handleUntrustPlugin removes the approval of the user for a plugin (S52a).
func (s *Server) handleUntrustPlugin(w http.ResponseWriter, r *http.Request) {
	p, store, ok := s.pluginFor(w, r, true)
	if !ok {
		return
	}
	removed, err := store.Untrust(s.dir, p.Name, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the approval cannot be removed")
		s.warn(fmt.Sprintf("untrust plugin %s: %v", p.Name, err))
		return
	}
	if removed {
		s.info(fmt.Sprintf("plugin %s no longer trusted", p.Name))
	}
	writeJSON(w, http.StatusOK, s.pluginInfo(r, p, store, s.pluginUses()))
}

// handleReloadPlugin checks and describes a plugin again, e.g. after it failed to start.
func (s *Server) handleReloadPlugin(w http.ResponseWriter, r *http.Request) {
	p, store, ok := s.pluginFor(w, r, false)
	if !ok {
		return
	}
	plugin.Forget(p)
	writeJSON(w, http.StatusOK, s.pluginInfo(r, p, store, s.pluginUses()))
}

// pluginFor returns the plugin named in the path and the approvals; with change, it
// refuses where the editor cannot change approvals.
func (s *Server) pluginFor(w http.ResponseWriter, r *http.Request, change bool) (*model.Plugin, plugintrust.Store, bool) {
	if change && !canTrust() {
		writeError(w, http.StatusForbidden, "Runnel runs as the Windows service: an administrator trusts plugins with runnel plugins trust --service")
		return nil, plugintrust.Store{}, false
	}
	plugins, _ := model.LoadPlugins(s.dir)
	p := plugins[r.PathValue("name")]
	if p == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("plugin %q not found, or its plugin.yaml has errors (see Problems)", r.PathValue("name")))
		return nil, plugintrust.Store{}, false
	}
	store, err := plugin.DefaultStore()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the approvals of plugins cannot be read")
		return nil, plugintrust.Store{}, false
	}
	return p, store, true
}
