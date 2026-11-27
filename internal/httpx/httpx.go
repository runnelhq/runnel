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

// ErrorBody is the body of error responses: {"error": "…"}, with a stable code for
// some of them (e.g. "no_project", R10).
type ErrorBody struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
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

// Numbers turns the numbers of a JSON value read with UseNumber into int64 or float64.
func Numbers(v any) any {
	switch v := v.(type) {
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n
		}
		f, _ := v.Float64()
		return f
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			out[k] = Numbers(x)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = Numbers(x)
		}
		return out
	}
	return v
}
