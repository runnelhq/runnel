package transfer

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
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

// session is one connection to a server for one execution of an action: SSH with SFTP or
// FTP with or without TLS. Cancelling the context of the execution closes it, so that no
// call on the server blocks the run.
type session struct {
	addr, user string
	fs         remoteFS
	stop       func() bool
	dead       atomic.Bool
}

// remoteFS is what the actions need from a server, whatever the protocol (sftpFS, ftpFS).
// Paths are POSIX paths; a missing entry is fs.ErrNotExist.
type remoteFS interface {
	Stat(p string) (fs.FileInfo, error)  // links count as what they point to where the server tells
	Lstat(p string) (fs.FileInfo, error) // links count as links
	ReadDir(dir string) ([]fs.FileInfo, error)
	MkdirAll(dir string) error
	Create(p string, r io.Reader) error // a new file with the content of r
	Retrieve(p string, w io.Writer) error
	Chtimes(p string, t time.Time) // where the server can; otherwise nothing
	Rename(from, to string, replace bool) error
	Remove(p string) error
	RemoveDir(p string) error
	Getwd() (string, error)
	Lost(err error) bool // the connection dropped
	Close()              // ends the connection; calls on the server fail from then on
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

// connect signs in to the server of a resolved connection of type sftp, ftps or ftp.
func connect(ctx context.Context, c connection.Resolved) (*session, error) {
	var s *session
	var err error
	switch c.Type {
	case connections.TypeSFTP:
		var cfg connections.SFTP
		if err := connections.Builtin().Decode(c, &cfg); err != nil {
			return nil, err
		}
		s, err = dial(ctx, cfg)
	case connections.TypeFTPS, connections.TypeFTP:
		cfg, cerr := ftpConfigOf(c)
		if cerr != nil {
			return nil, cerr
		}
		s, err = dialFTP(ctx, cfg)
	default:
		return nil, fmt.Errorf("connection %s has type %s, not sftp, ftps or ftp", c.Name, c.Type)
	}
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
	addr, timeout, err := address(cfg.Host, cfg.Port, "22", cfg.Timeout)
	if err != nil {
		return nil, err
	}
	want, err := fingerprints(cfg.HostKey)
	if err != nil {
		return nil, err
	}
	auth, err := signIn(cfg)
	if err != nil {
		return nil, err
	}
	s := &session{addr: addr, user: cfg.User}
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
		return nil, &signInError{fmt.Sprintf("sign-in to %s as %s failed: wrong user, password or key", s.addr, cfg.User)}
	}
	return nil, fmt.Errorf("cannot connect to %s: %w", s.addr, plainSSH(err))
}

// address checks host, port (empty: fallback) and timeout of a connection.
func address(host, port, fallback, timeout string) (string, time.Duration, error) {
	host, port = strings.TrimSpace(host), strings.TrimSpace(port)
	if host == "" {
		return "", 0, errors.New("host is empty")
	}
	if port == "" {
		port = fallback
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", 0, fmt.Errorf("port: %q is not a TCP port", port)
	}
	d := defaultConnectTimeout
	if strings.TrimSpace(timeout) != "" {
		v, err := model.Duration(timeout).Value()
		if err != nil || v <= 0 {
			return "", 0, fmt.Errorf("timeout: %q is not a duration like 15s", timeout)
		}
		d = v
	}
	return net.JoinHostPort(host, port), d, nil
}

// handshake speaks SSH on conn and starts SFTP.
func (s *session) handshake(conn net.Conn, config *ssh.ClientConfig) error {
	c, chans, reqs, err := ssh.NewClientConn(conn, s.addr, config)
	if err != nil {
		return err
	}
	f := &sftpFS{ssh: ssh.NewClient(c, chans, reqs)}
	s.fs = f
	if f.sftp, err = sftp.NewClient(f.ssh); err != nil {
		return fmt.Errorf("the server offers no SFTP: %w", err)
	}
	_, f.posixRename = f.sftp.HasExtension("posix-rename@openssh.com")
	return nil
}

// closeConn ends the connection; calls on the server fail from then on.
func (s *session) closeConn() {
	s.dead.Store(true)
	if s.fs != nil {
		s.fs.Close()
	}
}

// closed reports whether the connection was closed (timeout of a call, cancelled run).
func (s *session) closed() bool { return s.dead.Load() }

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

// signInError is a refused sign-in.
type signInError struct{ msg string }

func (e *signInError) Error() string { return e.msg }

// refused reports whether the server refused Runnel (host key, certificate, sign-in): trying again
// does not help and may lock the account.
func refused(err error) bool {
	_, hostKey := errors.AsType[*hostKeyError](err)
	_, signIn := errors.AsType[*signInError](err)
	_, cert := errors.AsType[*certError](err)
	return hostKey || signIn || cert
}

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
