package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
)

// seen records the requests of a test server.
type seen struct {
	mu   sync.Mutex
	reqs []*http.Request
}

func (s *seen) last() *http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reqs[len(s.reqs)-1]
}

// api starts a server that records requests and answers with status (or 401 for the
// bearer token "old").
func apiServer(t *testing.T) (*httptest.Server, *seen) {
	t.Helper()
	s := &seen{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.reqs = append(s.reqs, r)
		s.mu.Unlock()
		if r.Header.Get("Authorization") == "Bearer old" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, s
}

// withConnection runs http.request with the connection "api" of the given fields.
func withConnection(t *testing.T, conn map[string]any, with map[string]any) (map[string]any, error) {
	t.Helper()
	with["connection"] = "api"
	a, err := newRequest(with)
	if err != nil {
		return nil, err
	}
	ec := &action.ExecContext{
		Log: func(slog.Level, string) {},
		Connection: func(_ context.Context, name string) (connection.Resolved, error) {
			if name != "api" {
				return connection.Resolved{}, errors.New("unknown")
			}
			typ := connections.TypeHTTP
			if conn["type"] != nil {
				typ = conn["type"].(string)
				delete(conn, "type")
			}
			return connection.Resolved{Name: name, Type: typ, With: conn}, nil
		},
	}
	res, err := a.Execute(context.Background(), ec)
	out, _ := res.Output.(map[string]any)
	return out, err
}

func TestConnectionAddresses(t *testing.T) {
	srv, s := apiServer(t)
	for base, cases := range map[string]map[string]string{
		srv.URL + "/v1":  {"orders": "/v1/orders", "/orders": "/v1/orders", "": "/v1/", "orders/7?x=1": "/v1/orders/7?status=open&x=1", srv.URL + "/other": "/other"},
		srv.URL + "/v1/": {"orders": "/v1/orders", "../v2/x": "/v2/x"},
	} {
		for url, want := range cases {
			with := map[string]any{"url": url}
			if strings.Contains(url, "?") {
				with["query"] = map[string]any{"status": "open"}
			}
			if _, err := withConnection(t, map[string]any{"baseUrl": base}, with); err != nil {
				t.Fatalf("%s + %q: %v", base, url, err)
			}
			if got := s.last().URL.RequestURI(); got != want {
				t.Errorf("%s + %q: %s, want %s", base, url, got, want)
			}
		}
	}
}

func TestConnectionSignIn(t *testing.T) {
	srv, s := apiServer(t)
	cases := []struct {
		conn  map[string]any
		check func(r *http.Request) bool
	}{
		{map[string]any{"auth": "bearer", "token": "tok"}, func(r *http.Request) bool { return r.Header.Get("Authorization") == "Bearer tok" }},
		{map[string]any{"auth": "basic", "user": "ada", "password": "pw"}, func(r *http.Request) bool {
			u, p, ok := r.BasicAuth()
			return ok && u == "ada" && p == "pw"
		}},
		{map[string]any{"auth": "apiKey", "apiKey": "k1"}, func(r *http.Request) bool { return r.Header.Get("X-API-Key") == "k1" }},
		{map[string]any{"auth": "apiKey", "apiKey": "k2", "apiKeyName": "api_key", "apiKeyIn": "query"}, func(r *http.Request) bool {
			return r.URL.Query().Get("api_key") == "k2" && r.URL.Query().Get("a") == "1"
		}},
		// Headers of the connection, overridden by the action; the sign-in wins over both.
		{map[string]any{"auth": "bearer", "token": "tok", "headers": map[string]any{"Accept": "application/json", "X-Tenant": "7"}}, func(r *http.Request) bool {
			return r.Header.Get("Accept") == "application/json" && r.Header.Get("X-Tenant") == "8" && r.Header.Get("Authorization") == "Bearer tok"
		}},
	}
	for _, c := range cases {
		c.conn["baseUrl"] = srv.URL
		with := map[string]any{"url": "items?a=1", "headers": map[string]any{"X-Tenant": "8", "Authorization": "Bearer mine"}}
		if _, err := withConnection(t, c.conn, with); err != nil {
			t.Fatalf("%v: %v", c.conn, err)
		}
		if r := s.last(); !c.check(r) {
			t.Errorf("%v: %v %v", c.conn, r.URL, r.Header)
		}
	}
}

func TestConnectionOAuth2(t *testing.T) {
	api, s := apiServer(t)
	var calls atomic.Int32
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := "old"
		if calls.Add(1) > 1 {
			token = "new"
		}
		if r.FormValue("client_secret") != "app-secret" || r.FormValue("scope") != "api://erp/.default" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": token, "expires_in": 3600})
	}))
	t.Cleanup(tokens.Close)
	conn := func() map[string]any {
		return map[string]any{"baseUrl": api.URL, "auth": "oauth2", "tokenUrl": tokens.URL, "clientId": "erp", "clientSecret": "app-secret", "scope": "api://erp/.default"}
	}
	// The first token is refused (401): the action gets a new one and sends again.
	out, err := withConnection(t, conn(), map[string]any{"url": "orders"})
	if err != nil || out["status"] != int64(200) || s.last().Header.Get("Authorization") != "Bearer new" || calls.Load() != 2 {
		t.Fatalf("%v %v, %d token requests", out, err, calls.Load())
	}
	// The new token is kept.
	if _, err := withConnection(t, conn(), map[string]any{"url": "orders"}); err != nil || calls.Load() != 2 {
		t.Fatalf("%v, %d token requests", err, calls.Load())
	}
	bad := conn()
	bad["clientSecret"] = "wrong"
	if _, err := withConnection(t, bad, map[string]any{"url": "orders"}); err == nil || !strings.Contains(err.Error(), "connection api: token request refused (status 401): invalid_client") || strings.Contains(err.Error(), "wrong") {
		t.Errorf("error %v", err)
	}
}

func TestConnectionRejections(t *testing.T) {
	srv, s := apiServer(t)
	before := len(s.reqs)
	cases := []struct {
		conn map[string]any
		url  string
		want string
	}{
		{map[string]any{}, "https://evil.example.com/steal", "url: https://evil.example.com is not the host of connection api"},
		{map[string]any{}, "//evil.example.com/steal", "is not the host of connection api"},
		{map[string]any{"baseUrl": "https://api.example.com"}, srv.URL + "/x", "is not the host of connection api (https://api.example.com)"},
		{map[string]any{"baseUrl": "http://api.example.com", "auth": "bearer", "token": "t"}, "x", "the sign-in would be sent unencrypted"},
		{map[string]any{"baseUrl": "api.example.com"}, "x", `baseUrl: "api.example.com" is not an address`},
		{map[string]any{"auth": "bearer"}, "x", "auth bearer needs token"},
		{map[string]any{"auth": "basic"}, "x", "auth basic needs user"},
		{map[string]any{"auth": "apiKey"}, "x", "auth apiKey needs apiKey"},
		{map[string]any{"auth": "oauth2", "clientId": "c", "clientSecret": "s"}, "x", "auth oauth2 needs tokenUrl"},
		{map[string]any{"auth": "oauth2", "tokenUrl": "http://login.example.com/t", "clientId": "c", "clientSecret": "s"}, "x", "is not an address with https://"},
		{map[string]any{"auth": "digest"}, "x", `auth: unknown sign-in "digest"`},
		{map[string]any{"type": "smtp", "host": "mail"}, "x", "connection api has type smtp, not http"},
	}
	for _, c := range cases {
		if c.conn["baseUrl"] == nil && c.conn["type"] == nil {
			c.conn["baseUrl"] = srv.URL
		}
		if _, err := withConnection(t, c.conn, map[string]any{"url": c.url}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v %s: error %v, want %q", c.conn, c.url, err, c.want)
		}
	}
	if len(s.reqs) != before {
		t.Errorf("%d requests sent", len(s.reqs)-before)
	}
	if w := connections.Builtin().Warnings(connections.TypeHTTP, map[string]any{"baseUrl": "http://erp.local/api"}); !strings.Contains(w["baseUrl"], "not encrypted") {
		t.Errorf("warning %v", w)
	}
	if w := connections.Builtin().Warnings(connections.TypeHTTP, map[string]any{"baseUrl": "http://localhost:8080"}); w != nil {
		t.Errorf("warning for localhost %v", w)
	}
}

func TestConnectionWithoutProject(t *testing.T) {
	a, err := newRequest(map[string]any{"connection": "api", "url": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Execute(context.Background(), &action.ExecContext{Log: func(slog.Level, string) {}}); !errors.Is(err, connection.ErrNotAvailable) {
		t.Errorf("error %v", err)
	}
}

// Every page carries the sign-in of the connection; a next page on another host is not
// requested.
func TestConnectionPaginate(t *testing.T) {
	var srv *httptest.Server
	var auths []string
	var mu sync.Mutex
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		next := srv.URL + "/v1/items?cursor=2"
		if r.URL.Query().Get("cursor") == "2" {
			next = "https://evil.example.com/v1/items?cursor=3"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"value": []any{1}, "@odata.nextLink": next})
	}))
	t.Cleanup(srv.Close)
	conn := map[string]any{"baseUrl": srv.URL + "/v1", "auth": "bearer", "token": "tok"}
	_, err := withConnection(t, conn, map[string]any{"url": "items", "paginate": map[string]any{"style": "url", "items": "value", "next": "@odata.nextLink"}})
	if err == nil || !strings.Contains(err.Error(), "the next page is on https://evil.example.com") {
		t.Fatalf("error %v", err)
	}
	if len(auths) != 2 || auths[0] != "Bearer tok" || auths[1] != "Bearer tok" {
		t.Errorf("%v", auths)
	}
}
