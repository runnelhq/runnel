package transfer

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/model"
)

// defaultConnectTimeout limits connecting and signing in unless timeout of the connection
// says otherwise.
const defaultConnectTimeout = 15 * time.Second

// session is one SSH connection with SFTP for one execution of an action. Cancelling the
// context of the execution closes it, so that no call on the server blocks the run.
type session struct {
	addr, user  string
	ssh         *ssh.Client
	sftp        *sftp.Client
	posixRename bool
	stop        func() bool
}

// open resolves the connection of an action and connects to its server.
func open(ctx context.Context, ec *action.ExecContext, name string) (*session, error) {
	if ec.Connection == nil {
		return nil, connection.ErrNotAvailable
	}
	conn, err := ec.Connection(ctx, name)
	if err != nil {
		return nil, err
	}
	return connect(ctx, conn)
}

// connect signs in to the server of a resolved connection of type sftp.
func connect(ctx context.Context, c connection.Resolved) (*session, error) {
	if c.Type != connections.TypeSFTP {
		return nil, fmt.Errorf("connection %s has type %s, not sftp", c.Name, c.Type)
	}
	var cfg connections.SFTP
	if err := connections.Builtin().Decode(c, &cfg); err != nil {
		return nil, err
	}
	s, err := dial(ctx, cfg)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("connection %s: %w", c.Name, err)
	}
	s.stop = context.AfterFunc(ctx, s.closeConn)
	return s, nil
}

// dial connects, checks the host key and signs in, all within timeout of the connection.
func dial(ctx context.Context, cfg connections.SFTP) (*session, error) {
	host, port := strings.TrimSpace(cfg.Host), strings.TrimSpace(cfg.Port)
	if host == "" {
		return nil, errors.New("host is empty")
	}
	if port == "" {
		port = "22"
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return nil, fmt.Errorf("port: %q is not a TCP port", cfg.Port)
	}
	timeout := defaultConnectTimeout
	if strings.TrimSpace(cfg.Timeout) != "" {
		d, err := model.Duration(cfg.Timeout).Value()
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("timeout: %q is not a duration like 15s", cfg.Timeout)
		}
		timeout = d
	}
	want, err := fingerprints(cfg.HostKey)
	if err != nil {
		return nil, err
	}
	auth, err := signIn(cfg)
	if err != nil {
		return nil, err
	}
	s := &session{addr: net.JoinHostPort(host, port), user: cfg.User}
	config := &ssh.ClientConfig{
		User: cfg.User,
		Auth: auth,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			return checkHostKey(s.addr, key, want)
		},
		Timeout: timeout,
	}
	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dctx, "tcp", s.addr)
	if err != nil {
		if ctx.Err() == nil && dctx.Err() != nil {
			return nil, fmt.Errorf("cannot connect to %s within %s", s.addr, timeout)
		}
		return nil, fmt.Errorf("cannot connect to %s: %w", s.addr, plainNet(err))
	}
	// The handshake and the start of SFTP have no context of their own: closing the
	// connection ends them when the time is up or the run is cancelled.
	stop := context.AfterFunc(dctx, func() { _ = conn.Close() })
	err = s.handshake(conn, config)
	if !stop() {
		s.closeConn()
		_ = conn.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%s did not answer within %s", s.addr, timeout)
	}
	if err == nil {
		return s, nil
	}
	s.closeConn()
	_ = conn.Close()
	if ke, ok := errors.AsType[*hostKeyError](err); ok {
		return nil, ke
	}
	if strings.Contains(err.Error(), "unable to authenticate") {
		return nil, fmt.Errorf("sign-in to %s as %s failed: wrong user, password or key", s.addr, cfg.User)
	}
	return nil, fmt.Errorf("cannot connect to %s: %w", s.addr, plainSSH(err))
}

// handshake speaks SSH on conn and starts SFTP.
func (s *session) handshake(conn net.Conn, config *ssh.ClientConfig) error {
	c, chans, reqs, err := ssh.NewClientConn(conn, s.addr, config)
	if err != nil {
		return err
	}
	s.ssh = ssh.NewClient(c, chans, reqs)
	if s.sftp, err = sftp.NewClient(s.ssh); err != nil {
		return fmt.Errorf("the server offers no SFTP: %w", err)
	}
	_, s.posixRename = s.sftp.HasExtension("posix-rename@openssh.com")
	return nil
}

// closeConn ends the connection; calls on the server fail from then on.
func (s *session) closeConn() {
	if s.sftp != nil {
		_ = s.sftp.Close()
	}
	if s.ssh != nil {
		_ = s.ssh.Close()
	}
}

// close ends the session at the end of an execution.
func (s *session) close() {
	if s.stop != nil {
		s.stop()
	}
	s.closeConn()
}

// within runs fn, a call on the server, at most for timeout; then it closes the
// connection, which ends fn. what names the call for the message, e.g. "upload of a.csv".
func (s *session) within(ctx context.Context, timeout time.Duration, what string, fn func() error) error {
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stop := context.AfterFunc(tctx, s.closeConn)
	err := fn()
	if !stop() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%s did not finish within %s (timeout)", what, timeout)
	}
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// fingerprints reads hostKey: SHA256 fingerprints separated by spaces or commas, with or
// without "SHA256:", as ssh-keygen -l shows them. They are returned without the prefix.
func fingerprints(text string) ([]string, error) {
	var out []string
	for _, f := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		b64 := strings.TrimRight(strings.TrimPrefix(f, "SHA256:"), "=")
		if raw, err := base64.RawStdEncoding.DecodeString(b64); err != nil || len(raw) != 32 {
			return nil, fmt.Errorf("hostKey: %q is not a SHA256 fingerprint like SHA256:ExampleFingerprintOfTheServerHostKeyAbcdefg (ssh-keygen -l -E sha256)", f)
		}
		out = append(out, b64)
	}
	return out, nil
}

// checkHostKey accepts the key of the server only if its fingerprint is in hostKey; there
// is no "trust on first use" (ADR 0022). The messages name the fingerprint to check.
func checkHostKey(addr string, key ssh.PublicKey, want []string) error {
	got := ssh.FingerprintSHA256(key)
	if len(want) == 0 {
		return &hostKeyError{fmt.Sprintf("the host key of %s is not known: make sure with the administrator of the server that its fingerprint is %s (%s), then set it as hostKey of the connection", addr, got, key.Type())}
	}
	if slices.Contains(want, strings.TrimPrefix(got, "SHA256:")) {
		return nil
	}
	return &hostKeyError{fmt.Sprintf("the host key of %s (%s %s) does not match hostKey of the connection; the server may have been replaced or someone is in between: check the fingerprint with the administrator of the server before changing hostKey", addr, key.Type(), got)}
}

// hostKeyError is a refused host key.
type hostKeyError struct{ msg string }

func (e *hostKeyError) Error() string { return e.msg }

// signIn returns the methods of the sign-in: the key, then the password. The messages
// never contain the values.
func signIn(cfg connections.SFTP) ([]ssh.AuthMethod, error) {
	var out []ssh.AuthMethod
	if strings.TrimSpace(cfg.PrivateKey) != "" {
		key := []byte(cfg.PrivateKey)
		signer, err := ssh.ParsePrivateKey(key)
		if _, protected := errors.AsType[*ssh.PassphraseMissingError](err); protected && cfg.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(cfg.Passphrase))
		}
		_, protected := errors.AsType[*ssh.PassphraseMissingError](err)
		switch {
		case protected:
			return nil, errors.New("privateKey is protected by a passphrase; set passphrase")
		case errors.Is(err, x509.IncorrectPasswordError):
			return nil, errors.New("passphrase does not open privateKey")
		case err != nil:
			return nil, errors.New("privateKey is not a private key in PEM format (-----BEGIN … PRIVATE KEY-----)")
		}
		out = append(out, ssh.PublicKeys(signer))
	}
	if cfg.Password != "" {
		pw := cfg.Password
		// Some servers (e.g. with PAM) ask for the password as keyboard-interactive.
		answer := func(_, _ string, questions []string, echo []bool) ([]string, error) {
			switch {
			case len(questions) == 0:
				return nil, nil
			case len(questions) == 1 && !echo[0]:
				return []string{pw}, nil
			}
			return nil, errors.New("the server asks more than the password")
		}
		out = append(out, ssh.Password(pw), ssh.KeyboardInteractive(answer))
	}
	if len(out) == 0 {
		return nil, errors.New("no sign-in: set password or privateKey")
	}
	return out, nil
}

// plainNet returns the reason of a network error without operation and addresses.
func plainNet(err error) error {
	if se, ok := errors.AsType[*os.SyscallError](err); ok {
		return se.Err
	}
	if oe, ok := errors.AsType[*net.OpError](err); ok {
		return oe.Err
	}
	return err
}

// plainSSH returns an error of the SSH library without its "ssh: " prefixes.
func plainSSH(err error) error {
	msg := strings.ReplaceAll(err.Error(), "ssh: ", "")
	return errors.New(msg)
}
