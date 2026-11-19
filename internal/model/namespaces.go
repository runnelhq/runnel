package model

import (
	"slices"
	"strings"
)

// Type names of actions and transforms have the form "<namespace>.<name>", e.g.
// "file.copy". Actions and transforms have separate namespaces of names: the same
// type may exist as action (in workflows) and as transform (in pipelines), e.g.
// "http.request". A namespace belongs to exactly one provider (ADR 0013).

// reservedNamespaces belong to the actions and transforms built into Runnel,
// including those planned in the roadmap. Plugins must not use them.
var reservedNamespaces = []string{
	"ai", "core", "csv", "db", "excel", "file", "ftp", "http", "json",
	"mail", "mcp", "sftp", "shell", "xml", "zip",
}

// mcpNamespacePrefix starts the namespaces of tools provided by MCP connections,
// e.g. "mcp-github.create-issue" (ADR 0012, ADR 0013).
const mcpNamespacePrefix = "mcp-"

// ValidTypeName reports whether typ has the form "<namespace>.<name>"
// (lowercase letters, digits and "-", segments separated by ".").
func ValidTypeName(typ string) bool {
	return actionTypePattern.MatchString(typ)
}

// TypeNamespace returns the namespace of a type name: the part before the first ".".
func TypeNamespace(typ string) string {
	ns, _, _ := strings.Cut(typ, ".")
	return ns
}

// IsReservedNamespace reports whether a namespace is reserved for built-in
// actions and transforms or for MCP connections, and thus not available to plugins.
func IsReservedNamespace(ns string) bool {
	return slices.Contains(reservedNamespaces, ns) || strings.HasPrefix(ns, mcpNamespacePrefix)
}

// ReservedNamespaces returns the namespaces reserved for built-in actions and transforms.
func ReservedNamespaces() []string {
	return slices.Clone(reservedNamespaces)
}
