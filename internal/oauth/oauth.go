// Package oauth gets access tokens with the OAuth 2.0 client credentials grant
// (RFC 6749, section 4.4) for connections (ADR 0022), e.g. for mail through Office 365.
// Tokens stay in memory until shortly before they expire; they are never written to a
// file or a log.
package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/runnelhq/runnel/internal/httpx"
)

// ClientCredentials names an application that signs in by itself, without a user.
type ClientCredentials struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scope        string
}

// MicrosoftTokenURL returns the token address of a Microsoft Entra tenant (an ID or a
// domain such as contoso.onmicrosoft.com).
func MicrosoftTokenURL(tenant string) string {
	return "https://login.microsoftonline.com/" + url.PathEscape(strings.TrimSpace(tenant)) + "/oauth2/v2.0/token"
}

// renewBefore is how long before it expires a token is replaced.
const renewBefore = 5 * time.Minute

// maxAnswer limits the answer of the token server.
const maxAnswer = 1 << 20

// Client is used for the token requests; proxies come from the environment
// (HTTPS_PROXY). Tests replace it.
var Client = &http.Client{Transport: http.DefaultTransport}

type token struct {
	value   string
	expires time.Time
}

var (
	mu     sync.Mutex
	tokens = map[[32]byte]token{}
	now    = time.Now
)

// CheckURL accepts only https addresses, and http to this computer (test servers).
func CheckURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "https" && (u.Scheme != "http" || !httpx.IsLoopback(u.Host))) {
		return fmt.Errorf("tokenUrl: %q is not an address with https://", raw)
	}
	return nil
}

// Token returns an access token, from memory while it is valid for at least five more
// minutes. The request ends with ctx. Messages never contain the secret.
func Token(ctx context.Context, c ClientCredentials) (string, error) {
	if err := CheckURL(c.TokenURL); err != nil {
		return "", err
	}
	key := sha256.Sum256([]byte(c.TokenURL + "\x00" + c.ClientID + "\x00" + c.ClientSecret + "\x00" + c.Scope))
	mu.Lock()
	defer mu.Unlock()
	if t, ok := tokens[key]; ok && now().Add(renewBefore).Before(t.expires) {
		return t.value, nil
	}
	t, err := request(ctx, c)
	if err != nil {
		return "", err
	}
	tokens[key] = t
	return t.value, nil
}

// Forget drops the token of c, e.g. after the server refused it.
func Forget(c ClientCredentials) {
	key := sha256.Sum256([]byte(c.TokenURL + "\x00" + c.ClientID + "\x00" + c.ClientSecret + "\x00" + c.Scope))
	mu.Lock()
	delete(tokens, key)
	mu.Unlock()
}

func request(ctx context.Context, c ClientCredentials) (token, error) {
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {c.ClientID}, "client_secret": {c.ClientSecret}}
	if c.Scope != "" {
		form.Set("scope", c.Scope)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(c.TokenURL), strings.NewReader(form.Encode()))
	if err != nil {
		return token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	start := now()
	resp, err := Client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return token{}, ctx.Err()
		}
		return token{}, fmt.Errorf("token request: %w", errors.Unwrap(err))
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return token{}, fmt.Errorf("token request: %w", err)
	}
	var answer struct {
		AccessToken string          `json:"access_token"`
		ExpiresIn   json.RawMessage `json:"expires_in"`
		Error       string          `json:"error"`
		Description string          `json:"error_description"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return token{}, fmt.Errorf("token request: the server answered with status %d and no JSON", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || answer.AccessToken == "" {
		msg := answer.Error
		if d := firstLine(answer.Description); d != "" {
			msg += ": " + d
		}
		if msg == "" {
			msg = "no access_token in the answer"
		}
		return token{}, fmt.Errorf("token request refused (status %d): %s", resp.StatusCode, msg)
	}
	// expires_in is a number, at some servers a text; without it the token is used once.
	var seconds float64
	if err := json.Unmarshal(answer.ExpiresIn, &seconds); err != nil {
		var text string
		if json.Unmarshal(answer.ExpiresIn, &text) == nil {
			_, _ = fmt.Sscan(text, &seconds)
		}
	}
	return token{value: answer.AccessToken, expires: start.Add(time.Duration(seconds) * time.Second)}, nil
}

// firstLine keeps the first line of a description, without the trace and correlation
// IDs Microsoft adds below or behind it.
func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	line, _, _ = strings.Cut(line, " Trace ID:")
	return strings.TrimSpace(strings.TrimSuffix(line, "\r"))
}
