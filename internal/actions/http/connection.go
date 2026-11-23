package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/httpx"
	"github.com/runnelhq/runnel/internal/oauth"
)

// api is a resolved http connection (S63a): base address, headers and sign-in.
type api struct {
	name  string
	base  *url.URL
	cfg   connections.HTTP
	oauth *oauth.ClientCredentials
}

// connect resolves the connection of the action; nil without one.
func (a *requestAction) connect(ctx context.Context, ec *action.ExecContext) (*api, error) {
	if a.Connection == "" {
		return nil, nil
	}
	if ec.Connection == nil {
		return nil, connection.ErrNotAvailable
	}
	conn, err := ec.Connection(ctx, a.Connection)
	if err != nil {
		return nil, err
	}
	if conn.Type != connections.TypeHTTP {
		return nil, fmt.Errorf("connection %s has type %s, not http", conn.Name, conn.Type)
	}
	p := &api{name: conn.Name}
	if err := connections.Builtin().Decode(conn, &p.cfg); err != nil {
		return nil, err
	}
	if err := p.check(); err != nil {
		return nil, fmt.Errorf("connection %s: %w", conn.Name, err)
	}
	return p, nil
}

// check reads the base address and the fields of the sign-in.
func (p *api) check() error {
	base, err := url.Parse(strings.TrimSpace(p.cfg.BaseURL))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return fmt.Errorf("baseUrl: %q is not an address with http:// or https://", p.cfg.BaseURL)
	}
	if !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}
	p.base = base
	missing := func(field string) error { return fmt.Errorf("auth %s needs %s", p.cfg.Auth, field) }
	switch p.cfg.Auth {
	case "", "none":
		return nil
	case "bearer":
		if p.cfg.Token == "" {
			return missing("token")
		}
	case "basic":
		if p.cfg.User == "" {
			return missing("user")
		}
	case "apiKey":
		if p.cfg.APIKey == "" {
			return missing("apiKey")
		}
	case "oauth2":
		switch {
		case p.cfg.TokenURL == "":
			return missing("tokenUrl")
		case p.cfg.ClientID == "":
			return missing("clientId")
		case p.cfg.ClientSecret == "":
			return missing("clientSecret")
		}
		if err := oauth.CheckURL(p.cfg.TokenURL); err != nil {
			return err
		}
		p.oauth = &oauth.ClientCredentials{TokenURL: p.cfg.TokenURL, ClientID: p.cfg.ClientID, ClientSecret: p.cfg.ClientSecret, Scope: p.cfg.Scope}
	default:
		return fmt.Errorf("auth: unknown sign-in %q", p.cfg.Auth)
	}
	// Credentials never travel unencrypted, except to the own computer.
	if base.Scheme == "http" && !httpx.IsLoopback(base.Host) {
		return errors.New("the sign-in would be sent unencrypted; use an https:// baseUrl")
	}
	return nil
}

// resolve returns the address of a request: url relative to the base address (also
// with a leading /), or an absolute address on the same host. Another host is an error,
// so that the sign-in of the connection never goes elsewhere.
func (p *api) resolve(raw string) (*url.URL, error) {
	ref, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("url: %q is not an address", raw)
	}
	if ref.IsAbs() || ref.Host != "" {
		if !strings.EqualFold(ref.Scheme, p.base.Scheme) || !strings.EqualFold(ref.Host, p.base.Host) {
			return nil, fmt.Errorf("url: %s://%s is not the host of connection %s (%s://%s); its sign-in is only sent there", ref.Scheme, ref.Host, p.name, p.base.Scheme, p.base.Host)
		}
		return ref, nil
	}
	ref.Path = strings.TrimPrefix(ref.Path, "/")
	ref.RawPath = ""
	return p.base.ResolveReference(ref), nil
}

// headers sets the headers of the connection; those of the action follow and win.
func (p *api) headers(req *http.Request) {
	for k, v := range p.cfg.Headers {
		req.Header.Set(k, v)
	}
}

// signIn sets the sign-in of the connection on a request; it wins over headers of the
// action.
func (p *api) signIn(ctx context.Context, req *http.Request) error {
	switch p.cfg.Auth {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+p.cfg.Token)
	case "basic":
		req.SetBasicAuth(p.cfg.User, p.cfg.Password)
	case "apiKey":
		name := p.cfg.APIKeyName
		if name == "" {
			name = "X-API-Key"
		}
		if p.cfg.APIKeyIn == "query" {
			q := req.URL.Query()
			q.Set(name, p.cfg.APIKey)
			req.URL.RawQuery = q.Encode()
		} else {
			req.Header.Set(name, p.cfg.APIKey)
		}
	case "oauth2":
		token, err := oauth.Token(ctx, *p.oauth)
		if err != nil {
			return fmt.Errorf("connection %s: %w", p.name, err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return nil
}

// refused drops an OAuth2 token the API refused (401) and reports whether the request
// should be sent once more with a new token.
func (p *api) refused(status int) bool {
	if p == nil || p.oauth == nil || status != http.StatusUnauthorized {
		return false
	}
	oauth.Forget(*p.oauth)
	return true
}
