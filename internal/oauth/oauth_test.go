package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// server answers each token request with the given status and body; %d in the body
// becomes the number of the request.
func server(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if r.Method != http.MethodPost || r.FormValue("grant_type") != "client_credentials" || r.FormValue("client_id") != "app" ||
			r.FormValue("client_secret") != "s3cret" || r.FormValue("scope") != "api/.default" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.WriteHeader(status)
		if strings.Contains(body, "%d") {
			_, _ = fmt.Fprintf(w, body, n)
		} else {
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func credentials(url string) ClientCredentials {
	return ClientCredentials{TokenURL: url, ClientID: "app", ClientSecret: "s3cret", Scope: "api/.default"}
}

func TestTokenIsKeptUntilShortlyBeforeExpiry(t *testing.T) {
	srv, calls := server(t, http.StatusOK, `{"access_token":"tok-%d","expires_in":3600}`)
	clock := time.Now()
	now = func() time.Time { return clock }
	t.Cleanup(func() { now = time.Now })
	c := credentials(srv.URL)
	for _, want := range []string{"tok-1", "tok-1"} {
		if tok, err := Token(context.Background(), c); err != nil || tok != want {
			t.Fatalf("%q %v, want %s", tok, err, want)
		}
	}
	clock = clock.Add(56 * time.Minute) // less than five minutes left
	if tok, _ := Token(context.Background(), c); tok != "tok-2" || calls.Load() != 2 {
		t.Errorf("%q after %d requests", tok, calls.Load())
	}
	Forget(c)
	if tok, _ := Token(context.Background(), c); tok != "tok-3" {
		t.Errorf("after Forget: %q", tok)
	}
}

func TestTokenExpiresInAsText(t *testing.T) {
	srv, calls := server(t, http.StatusOK, `{"access_token":"tok-%d","expires_in":"3600"}`)
	c := credentials(srv.URL)
	_, _ = Token(context.Background(), c)
	if tok, _ := Token(context.Background(), c); tok != "tok-1" || calls.Load() != 1 {
		t.Errorf("%q after %d requests", tok, calls.Load())
	}
}

func TestTokenErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{http.StatusUnauthorized, `{"error":"invalid_client","error_description":"AADSTS7000215: Invalid client secret. Trace ID: 42 Correlation ID: 7"}`, "token request refused (status 401): invalid_client: AADSTS7000215: Invalid client secret."},
		{http.StatusBadRequest, `{"error":"invalid_scope","error_description":"AADSTS70011: The scope is not valid.\r\nTrace ID: 42\r\n"}`, "token request refused (status 400): invalid_scope: AADSTS70011: The scope is not valid."},
		{http.StatusOK, `{"token_type":"Bearer"}`, "token request refused (status 200): no access_token in the answer"},
		{http.StatusBadGateway, `<html>proxy</html>`, "the server answered with status 502 and no JSON"},
	}
	for _, c := range cases {
		srv, _ := server(t, c.status, c.body)
		_, err := Token(context.Background(), credentials(srv.URL))
		if err == nil || !strings.HasSuffix(err.Error(), c.want) {
			t.Errorf("%s: error %v, want %q", c.body, err, c.want)
		}
		if err != nil && strings.Contains(err.Error(), "s3cret") {
			t.Error("secret in the message")
		}
	}
}

func TestTokenCancelled(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }))
	t.Cleanup(func() { close(block); srv.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Token(ctx, credentials(srv.URL)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error %v", err)
	}
}

func TestCheckURL(t *testing.T) {
	for url, ok := range map[string]bool{
		"https://login.microsoftonline.com/contoso.onmicrosoft.com/oauth2/v2.0/token": true,
		"http://127.0.0.1:8080/token":    true,
		"http://localhost/token":         true,
		"http://login.example.com/token": false,
		"ftp://login.example.com":        false,
		"login.example.com/token":        false,
	} {
		if err := CheckURL(url); (err == nil) != ok {
			t.Errorf("%s: %v", url, err)
		}
	}
	if got := MicrosoftTokenURL(" contoso.onmicrosoft.com "); got != "https://login.microsoftonline.com/contoso.onmicrosoft.com/oauth2/v2.0/token" {
		t.Error(got)
	}
}
