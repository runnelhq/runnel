package model

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Connections (ADR 0022): reusable settings of a database, server or API in
// connections/<name>.yaml. The fields under with: depend on the type; their schema
// comes from the registry of connection types (internal/connection), which also
// checks them (runs.CheckConnections).
const (
	KindConnection = "Connection"
	ConnectionsDir = "connections"
)

// Connection is the content of a file in connections/.
type Connection struct {
	Kind        string         `yaml:"kind" json:"kind" doc:"Type of the file" schema:"required,enum=Connection"`
	APIVersion  string         `yaml:"apiVersion" json:"apiVersion" doc:"Version of the file format" schema:"required,enum=runnel/v1"`
	Name        string         `yaml:"name" json:"name" doc:"Name of the connection; must match the file name, used as connection: <name>" schema:"required,example=erp-db"`
	Description string         `yaml:"description,omitempty" json:"description,omitempty" doc:"What the connection is for, e.g. ERP database, read only"`
	Type        string         `yaml:"type" json:"type" doc:"Kind of connection, e.g. sqlite; it decides the fields under with:" schema:"required,example=sqlite"`
	With        map[string]any `yaml:"with,omitempty" json:"with,omitempty" doc:"Settings of the connection type; differences between environments come from variables, e.g. ${DB_HOST}"`

	src *source
}

// ReadConnection parses and checks a connection file; name is the file (its base name
// must match the name of the connection) and is used in messages, e.g. the text shown in
// the editor. The fields under with: are checked against the type by the caller.
func ReadConnection(name string, data []byte) (*Connection, error) {
	src, err := parseSource(name, data)
	if err != nil {
		return nil, err
	}
	c := &Connection{src: src}
	if err := src.decode(c); err != nil {
		return nil, err
	}
	errs := checkHeader(src, c.Kind, KindConnection, c.APIVersion, c.Name)
	if want := strings.TrimSuffix(filepath.Base(name), ".yaml"); c.Name != "" && c.Name != want {
		errs = append(errs, src.errorf("$.name", "connection name %q must match the file name (%q)", c.Name, want))
	}
	if c.Type == "" {
		errs = append(errs, src.errorf("$", `missing required field "type" (the kind of connection, e.g. sqlite)`))
	}
	errs = append(errs, checkReferenceSyntax(src)...)
	return c, errs.errOrNil()
}

// LoadConnections reads all files in connections/ of a project; problems of all files
// are returned at once as Errors, the connections without errors by name.
func LoadConnections(dir string) (map[string]*Connection, error) {
	files, err := filepath.Glob(filepath.Join(dir, ConnectionsDir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	out := map[string]*Connection{}
	var errs Errors
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		c, err := ReadConnection(filepath.ToSlash(file), data)
		if es, ok := err.(Errors); ok { //nolint:errorlint // ReadConnection returns Errors itself
			errs = append(errs, es...)
			if es.HasErrors() {
				continue
			}
		} else if err != nil {
			return nil, err
		}
		out[c.Name] = c
	}
	return out, errs.errOrNil()
}

// ErrorAt returns an error at a path of the connection file, e.g. "$.with.path".
func (c *Connection) ErrorAt(path, format string, args ...any) *Error {
	return c.src.errorf(path, format, args...)
}

// CheckSecrets reports ${secret:…} outside the fields for secrets of the connection type
// (isSecret tells whether a field under with: holds a secret) and secrets whose store
// cannot be read with the given default stores.
func (c *Connection) CheckSecrets(isSecret func(field string) bool, stores []string) Errors {
	return checkSecrets(c.src, func(path string) bool {
		field, ok := strings.CutPrefix(path, "$.with.")
		return ok && !strings.ContainsAny(field, ".[") && isSecret != nil && isSecret(field)
	}, stores)
}

// CheckVariables reports references to variables that are not defined in one or more
// of the contexts (warnings), like Workflow.CheckVariables.
func (c *Connection) CheckVariables(contexts []VarContext) Errors {
	return checkVarRefs(c.src, contexts)
}

// File returns the path of the connection file as read.
func (c *Connection) File() string { return c.src.name }

// ConnectionScope returns the variables a connection is resolved with: those of the
// project and the environment (no workflow parameters), with its secret stores.
func ConnectionScope(p *Project, env *Environment) VarContext {
	name := ""
	if env != nil {
		name = env.Name
	}
	return VarContext{Environment: name, Scope: scopeFor(nil, p, env, nil)}
}

// ConnectionContexts returns one variable context per environment of the project (or
// only the named one), as VarContexts does for workflows.
func ConnectionContexts(p *Project, only string) ([]VarContext, error) {
	if p == nil {
		return []VarContext{ConnectionScope(nil, nil)}, nil
	}
	names := p.EnvironmentNames()
	if only != "" {
		if _, err := p.SelectEnvironment(only); err != nil {
			return nil, err
		}
		names = []string{only}
	}
	if len(names) == 0 {
		return []VarContext{ConnectionScope(p, nil)}, nil
	}
	out := make([]VarContext, len(names))
	for i, n := range names {
		out[i] = ConnectionScope(p, p.Environments[n])
	}
	return out, nil
}

// connectionFile returns the path of a connection file of a project for messages.
func connectionFile(dir, name string) string {
	return filepath.ToSlash(filepath.Join(dir, ConnectionsDir, name+".yaml"))
}

// UnknownConnection is the message for connection: naming no file in connections/.
func UnknownConnection(p *Project, name string) string {
	names := p.ConnectionNames()
	hint := "the project has no connections"
	if len(names) > 0 {
		hint = "connections: " + strings.Join(names, ", ")
	}
	if p != nil {
		return fmt.Sprintf("connection %q not found (expected %s; %s)", name, connectionFile(p.Dir, name), hint)
	}
	return fmt.Sprintf("connection %q not found (%s)", name, hint)
}

// ConnectionNames returns the names of the connections of the project, sorted.
func (p *Project) ConnectionNames() []string {
	if p == nil {
		return nil
	}
	names := make([]string, 0, len(p.Connections))
	for n := range p.Connections {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}
