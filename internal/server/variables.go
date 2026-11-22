package server

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/model"
)

// Limits of the scan for /variables: larger files and files beyond the count are left
// out, so that a large folder in the project cannot stall the editor.
var (
	maxVariableFiles    = 5000
	maxVariableFileSize = int64(1 << 20)
)

// variablesInfo answers GET /variables (E18a): every variable the project uses as
// ${NAME} or defines, where it is set (project.yaml, each environment) and where it is
// used, for the "Variables" list of the editor (mockup E18).
type variablesInfo struct {
	Environments       []string       `json:"environments"`
	DefaultEnvironment string         `json:"defaultEnvironment,omitempty"`
	Variables          []variableInfo `json:"variables"`
	// Truncated is set when files were left out because of the limits.
	Truncated bool `json:"truncated,omitempty"`
}

type variableInfo struct {
	Name string `json:"name"`
	// Project is the definition in project.yaml; nil if there is none.
	Project *variableValue `json:"project"`
	// Environments holds the definitions by environment; environments without one are
	// missing from the map.
	Environments map[string]*variableValue `json:"environments"`
	Uses         []variableUse             `json:"uses"`
}

type variableValue struct {
	// Value is the text as written in the file, with its own ${…} unresolved.
	Value string `json:"value"`
	File  string `json:"file"`
	Line  int    `json:"line"`
}

type variableUse struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	// Parameter is set when the file declares a parameter of this name: the use is
	// covered by the parameter and needs no project variable.
	Parameter bool `json:"parameter,omitempty"`
}

func (s *Server) handleVariables(w http.ResponseWriter, _ *http.Request) {
	info := variablesInfo{Environments: []string{}, Variables: []variableInfo{}}
	byName := map[string]*variableInfo{}
	get := func(name string) *variableInfo {
		v := byName[name]
		if v == nil {
			v = &variableInfo{Name: name, Environments: map[string]*variableValue{}, Uses: []variableUse{}}
			byName[name] = v
		}
		return v
	}
	if p, _ := model.LoadProject(s.dir); p != nil {
		// A project without environments lists none: [] in JSON, not null (the editor reads the list).
		info.Environments = append(info.Environments, p.EnvironmentNames()...)
		info.DefaultEnvironment = p.DefaultEnvironment
		for name, value := range p.Variables {
			get(name).Project = &variableValue{Value: value, File: model.ProjectFile, Line: p.VariablePos(name).Line}
		}
		for _, env := range info.Environments {
			e := p.Environments[env]
			for name, value := range e.Variables {
				get(name).Environments[env] = &variableValue{Value: value, File: path.Join(model.EnvironmentsDir, env+".yaml"), Line: e.VariablePos(name).Line}
			}
		}
	}

	files := 0
	err := fs.WalkDir(s.root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != "." && (strings.HasPrefix(d.Name(), ".") || d.Type()&fs.ModeSymlink != 0) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(path.Ext(p))
		if d.IsDir() || (ext != ".yaml" && ext != ".yml") {
			return nil
		}
		if files++; files > maxVariableFiles {
			info.Truncated = true
			return fs.SkipAll
		}
		if fi, err := d.Info(); err != nil || fi.Size() > maxVariableFileSize {
			info.Truncated = true
			return nil
		}
		data, err := fs.ReadFile(s.root.FS(), p)
		if err != nil {
			return nil
		}
		uses, params, err := model.ScanVariables(p, data)
		if err != nil {
			return nil // a broken file has no uses; validate reports it
		}
		for _, u := range uses {
			v := get(u.Name)
			v.Uses = append(v.Uses, variableUse{File: p, Line: u.Pos.Line, Column: u.Pos.Column, Parameter: slices.Contains(params, u.Name)})
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.SkipAll) {
		writeErr(w, err)
		return
	}
	for _, v := range byName {
		info.Variables = append(info.Variables, *v)
	}
	slices.SortFunc(info.Variables, func(a, b variableInfo) int { return strings.Compare(a.Name, b.Name) })
	writeJSON(w, http.StatusOK, info)
}
