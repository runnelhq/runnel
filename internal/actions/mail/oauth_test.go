package mail

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// tokenServer answers token requests with the tokens in turn: "secret-pw" is the one
// the SMTP test server accepts.
func tokenServer(t *testing.T, tokens ...string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1))
		if r.FormValue("client_secret") != "app-secret" || r.FormValue("scope") != "https://outlook.office365.com/.default" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client", "error_description": "AADSTS7000215: Invalid client secret provided.\r\nTrace ID: 1"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": tokens[min(n, len(tokens))-1], "expires_in": 3599, "token_type": "Bearer"})
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func oauthConnection(srv *testServer, tokenURL, secret string) map[string]any {
	return map[string]any{"host": "127.0.0.1", "port": srv.port(), "auth": "oauth2", "user": "runnel@example.com",
		"clientId": "app-" + secret, "clientSecret": secret, "tokenUrl": tokenURL, "from": "runnel@example.com"}
}

func TestSendOAuth2(t *testing.T) {
	srv := &testServer{tls: certificate(t), ext: []string{"STARTTLS", "AUTH LOGIN XOAUTH2"}}
	srv.start(t)
	tokens, calls := tokenServer(t, "secret-pw")
	conn := oauthConnection(srv, tokens.URL, "app-secret")
	for range 2 {
		if _, _, err := send(context.Background(), t, conn, map[string]any{"to": "ops@example.com", "subject": "Hi"}); err != nil {
			t.Fatal(err)
		}
	}
	if srv.mech != "XOAUTH2" || srv.user != "runnel@example.com" || srv.password != "secret-pw" {
		t.Errorf("auth %s %s %s", srv.mech, srv.user, srv.password)
	}
	if calls.Load() != 1 {
		t.Errorf("%d token requests, want 1 (the token is kept)", calls.Load())
	}
}

func TestSendOAuth2RefusedToken(t *testing.T) {
	srv := &testServer{tls: certificate(t), ext: []string{"STARTTLS", "AUTH XOAUTH2"}}
	srv.start(t)
	tokens, calls := tokenServer(t, "revoked", "secret-pw")
	conn := oauthConnection(srv, tokens.URL, "app-secret")
	conn["clientId"] = "refused"
	_, _, err := send(context.Background(), t, conn, map[string]any{"to": "ops@example.com", "subject": "Hi"})
	if err == nil || !strings.Contains(err.Error(), "sign-in failed: 535") || strings.Contains(err.Error(), "revoked") {
		t.Fatalf("error %v", err)
	}
	// The refused token is dropped: the next mail gets a new one.
	if _, _, err := send(context.Background(), t, conn, map[string]any{"to": "ops@example.com", "subject": "Hi"}); err != nil || calls.Load() != 2 {
		t.Fatalf("%v, %d token requests", err, calls.Load())
	}
}

func TestSendOAuth2Rejections(t *testing.T) {
	srv := &testServer{tls: certificate(t), ext: []string{"STARTTLS", "AUTH LOGIN"}}
	srv.start(t)
	tokens, _ := tokenServer(t, "secret-pw")
	cases := []struct {
		change map[string]any
		want   string
	}{
		{map[string]any{"clientSecret": "wrong"}, "token request refused (status 401): invalid_client: AADSTS7000215: Invalid client secret provided."},
		{nil, "the server does not offer the sign-in XOAUTH2"},
		{map[string]any{"user": ""}, "auth oauth2 needs user"},
		{map[string]any{"clientSecret": ""}, "auth oauth2 needs clientSecret"},
		{map[string]any{"tokenUrl": ""}, "auth oauth2 needs tenant or tokenUrl"},
		{map[string]any{"tokenUrl": "http://login.example.com/token"}, "is not an address with https://"},
		{map[string]any{"host": "smtp.example.com", "tls": "none"}, "the token would be sent unencrypted"},
	}
	for _, c := range cases {
		conn := oauthConnection(srv, tokens.URL, "app-secret")
		maps.Copy(conn, c.change)
		_, _, err := send(context.Background(), t, conn, map[string]any{"to": "ops@example.com", "subject": "Hi"})
		if err == nil || !strings.Contains(err.Error(), c.want) || strings.Contains(err.Error(), "app-secret") {
			t.Errorf("%v: error %v, want %q", c.change, err, c.want)
		}
	}
}
