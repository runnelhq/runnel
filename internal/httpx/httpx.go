// Package httpx holds small helpers shared by the HTTP servers of Runnel: the API for
// the editor (internal/server) and the listener of http triggers (internal/triggers).
package httpx

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

// IsLoopback reports whether a host – with or without port, e.g. from a Host header or
// a listen address – names this computer: localhost, 127.0.0.0/8 or ::1.
func IsLoopback(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// ErrorBody is the body of error responses: {"error": "…"}.
type ErrorBody struct {
	Error string `json:"error"`
}

// WriteJSON writes v as JSON with the status; indent makes it readable (API for the editor).
func WriteJSON(w http.ResponseWriter, status int, v any, indent bool) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	if indent {
		enc.SetIndent("", "  ")
	}
	_ = enc.Encode(v)
}
