package mail

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/httpx"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/oauth"
)

// rootCAs are the certificates trusted for the mail server; nil: those of the system.
// Tests set their own.
var rootCAs *x509.CertPool

// server is a resolved smtp connection.
type server struct {
	name     string // name of the connection, for messages
	host     string
	addr     string // host:port
	tls      string // starttls, tls or none
	user     string
	password string
	oauth    *oauth.ClientCredentials // with auth oauth2
	timeout  time.Duration
}

func newServer(name string, c connections.SMTP) (*server, error) {
	s := &server{name: name, host: strings.TrimSpace(c.Host), tls: c.TLS, user: c.User, password: c.Password, timeout: time.Minute}
	if s.tls == "" {
		s.tls = "starttls"
	}
	port := strings.TrimSpace(c.Port)
	if port == "" {
		port = map[string]string{"starttls": "587", "tls": "465", "none": "25"}[s.tls]
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return nil, fmt.Errorf("port %q is not a number from 1 to 65535", c.Port)
	}
	s.addr = net.JoinHostPort(s.host, port)
	if c.Timeout != "" {
		d, err := model.Duration(c.Timeout).Value()
		if err != nil {
			return nil, fmt.Errorf("timeout: %w", err)
		}
		s.timeout = d
	}
	switch auth := c.Auth; {
	case auth == "none":
		s.user, s.password = "", ""
	case auth == "oauth2":
		if err := s.useOAuth(c); err != nil {
			return nil, err
		}
	case auth == "password" || c.User != "":
		if c.User == "" {
			return nil, errors.New(`auth password needs a user`)
		}
		// The password never travels unencrypted, except to the own computer.
		if s.tls == "none" && !httpx.IsLoopback(s.addr) {
			return nil, errors.New("the password would be sent unencrypted; use tls: starttls or tls, or auth: none for a relay")
		}
	}
	return s, nil
}

// useOAuth checks the fields of auth oauth2 (S62b): the token is a credential like a
// password and never travels unencrypted, except to the own computer.
func (s *server) useOAuth(c connections.SMTP) error {
	s.password = ""
	missing := func(field string) error { return fmt.Errorf("auth oauth2 needs %s", field) }
	switch {
	case c.User == "":
		return missing("user (the mailbox that sends)")
	case c.ClientID == "":
		return missing("clientId")
	case c.ClientSecret == "":
		return missing("clientSecret")
	case c.Tenant == "" && c.TokenURL == "":
		return missing("tenant or tokenUrl")
	case s.tls == "none" && !httpx.IsLoopback(s.addr):
		return errors.New("the token would be sent unencrypted; use tls: starttls or tls")
	}
	cc := &oauth.ClientCredentials{TokenURL: c.TokenURL, ClientID: c.ClientID, ClientSecret: c.ClientSecret, Scope: c.Scope}
	if cc.TokenURL == "" {
		cc.TokenURL = oauth.MicrosoftTokenURL(c.Tenant)
	}
	if cc.Scope == "" {
		cc.Scope = connections.DefaultSMTPScope
	}
	if err := oauth.CheckURL(cc.TokenURL); err != nil {
		return err
	}
	s.oauth = cc
	return nil
}

// send delivers a mail to the recipients. The connection ends with the context, and
// the whole dialog takes at most the timeout of the connection.
func (s *server) send(parent context.Context, from string, to []string, data []byte) error {
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()
	secret := s.password
	if s.oauth != nil {
		token, err := oauth.Token(ctx, *s.oauth)
		if err != nil {
			return s.fail(parent, err)
		}
		secret = token
	}
	cfg := &tls.Config{ServerName: s.host, RootCAs: rootCAs, MinVersion: tls.VersionTLS12}
	var conn net.Conn
	var err error
	if s.tls == "tls" {
		conn, err = (&tls.Dialer{Config: cfg}).DialContext(ctx, "tcp", s.addr)
	} else {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", s.addr)
	}
	if err != nil {
		return s.fail(parent, err)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	// The run ends the connection through the context; the deadline is only the timeout
	// of the connection, so that an ended run reports its own error.
	_ = conn.SetDeadline(time.Now().Add(s.timeout))
	c, err := smtp.NewClient(conn, s.host)
	if err != nil {
		_ = conn.Close()
		return s.fail(parent, err)
	}
	defer func() { _ = c.Close() }()
	if err := s.dialog(c, cfg, secret, from, to, data); err != nil {
		return s.fail(parent, err)
	}
	return nil
}

func (s *server) dialog(c *smtp.Client, cfg *tls.Config, secret, from string, to []string, data []byte) error {
	if err := c.Hello(localName()); err != nil {
		return err
	}
	if s.tls == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("the server does not offer STARTTLS; use tls: tls (port 465) or ask the administrator of the mail server")
		}
		if err := c.StartTLS(cfg); err != nil {
			return err
		}
	}
	if ok, size := c.Extension("SIZE"); ok {
		if limit, err := strconv.Atoi(size); err == nil && limit > 0 && len(data) > limit {
			return fmt.Errorf("the mail has %d KB, the server accepts at most %d KB", len(data)>>10, limit>>10)
		}
	}
	if s.user != "" {
		ok, mechs := c.Extension("AUTH")
		if !ok {
			return errors.New("the server does not offer a sign-in; use auth: none")
		}
		a, err := s.auth(mechs, secret)
		if err != nil {
			return err
		}
		if err := c.Auth(a); err != nil {
			if s.oauth != nil {
				// A token the server refuses (e.g. revoked) is not used again.
				oauth.Forget(*s.oauth)
			}
			return fmt.Errorf("sign-in failed: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return fmt.Errorf("recipient %s: %w", r, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// fail names the connection in an error and explains a timeout; a cancelled or timed
// out run (parent) returns its own error.
func (s *server) fail(parent context.Context, err error) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("connection %s: no answer from %s within %s", s.name, s.addr, s.timeout)
	}
	return fmt.Errorf("connection %s: %w", s.name, err)
}

// localName is the name of this computer for the greeting (EHLO).
func localName() string {
	if h, err := os.Hostname(); err == nil && h != "" && !strings.ContainsAny(h, " \r\n") {
		return h
	}
	return "localhost"
}

// auth returns the sign-in for the mechanisms the server offers.
func (s *server) auth(mechs, secret string) (smtp.Auth, error) {
	if s.oauth == nil {
		return newAuth(mechs, s.user, secret), nil
	}
	if !slices.Contains(strings.Fields(strings.ToUpper(mechs)), "XOAUTH2") {
		return nil, errors.New("the server does not offer the sign-in XOAUTH2 for auth oauth2")
	}
	return &xoauth2{user: s.user, token: secret}, nil
}

// xoauth2 signs in with an access token (SASL XOAUTH2, as used by Office 365).
type xoauth2 struct{ user, token string }

func (a *xoauth2) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + a.user + "\x01auth=Bearer " + a.token + "\x01\x01"), nil
}

// Next answers an error of the server (a JSON text) with an empty line, after which
// the server ends the sign-in with its error code.
func (a *xoauth2) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return []byte{}, nil
	}
	return nil, nil
}

// auth signs in with PLAIN or, if the server offers only that, LOGIN (e.g. Exchange).
// It runs only on encrypted connections or to the own computer (checked in newServer).
type auth struct {
	mech, user, password string
	step                 int
}

func newAuth(mechs, user, password string) *auth {
	mech := "PLAIN"
	list := strings.Fields(strings.ToUpper(mechs))
	if !slices.Contains(list, "PLAIN") && slices.Contains(list, "LOGIN") {
		mech = "LOGIN"
	}
	return &auth{mech: mech, user: user, password: password}
}

func (a *auth) Start(*smtp.ServerInfo) (string, []byte, error) {
	if a.mech == "LOGIN" {
		return "LOGIN", nil, nil
	}
	return "PLAIN", []byte("\x00" + a.user + "\x00" + a.password), nil
}

func (a *auth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	if a.mech != "LOGIN" {
		return nil, errors.New("unexpected question of the server")
	}
	a.step++
	switch a.step {
	case 1:
		return []byte(a.user), nil
	case 2:
		return []byte(a.password), nil
	}
	return nil, fmt.Errorf("unexpected question of the server: %q", fromServer)
}
