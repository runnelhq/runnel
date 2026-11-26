package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net"
	"net/textproto"
	"path"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/jlaffaye/ftp"

	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
)

// rootCAs are the certificates an ftps server may be signed by; nil: those of the system
// (tests set their own).
var rootCAs *x509.CertPool

// ftpConfig are the fields of an ftps or ftp connection.
type ftpConfig struct {
	host, port, user, password, timeout string
	tls                                 string // explicit or implicit; empty for ftp
	serverName, fingerprint             string
}

// ftpConfigOf reads the fields of an ftps or ftp connection; ftp needs insecure: true.
func ftpConfigOf(c connection.Resolved) (ftpConfig, error) {
	if c.Type == connections.TypeFTP {
		var cfg connections.FTP
		if err := connections.Builtin().Decode(c, &cfg); err != nil {
			return ftpConfig{}, err
		}
		if !cfg.Insecure {
			return ftpConfig{}, fmt.Errorf("connection %s: ftp sends the password and all data unencrypted; use type ftps, or set insecure: true for a server without TLS in a trusted network", c.Name)
		}
		return ftpConfig{host: cfg.Host, port: cfg.Port, user: cfg.User, password: cfg.Password, timeout: cfg.Timeout}, nil
	}
	var cfg connections.FTPS
	if err := connections.Builtin().Decode(c, &cfg); err != nil {
		return ftpConfig{}, err
	}
	mode := cfg.TLS
	switch mode {
	case "":
		mode = "explicit"
	case "explicit", "implicit":
	default:
		return ftpConfig{}, fmt.Errorf("connection %s: tls: %q is not explicit or implicit", c.Name, cfg.TLS)
	}
	return ftpConfig{host: cfg.Host, port: cfg.Port, user: cfg.User, password: cfg.Password, timeout: cfg.Timeout,
		tls: mode, serverName: cfg.ServerName, fingerprint: cfg.Fingerprint}, nil
}

// dialFTP connects, starts TLS (ftps), checks the certificate and signs in, all within
// timeout of the connection. Transfers use passive mode only (EPSV, else PASV with the
// address of the server, never the one in its answer).
func dialFTP(ctx context.Context, cfg ftpConfig) (*session, error) {
	fallback := "21"
	if cfg.tls == "implicit" {
		fallback = "990"
	}
	addr, timeout, err := address(cfg.host, cfg.port, fallback, cfg.timeout)
	if err != nil {
		return nil, err
	}
	f := &ftpFS{conns: map[*trackedConn]struct{}{}}
	var opts []ftp.DialOption
	var tc *tls.Config
	if cfg.tls != "" {
		pins, err := certPins(cfg.fingerprint)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(cfg.serverName)
		if name == "" {
			name, _, _ = net.SplitHostPort(addr)
		}
		tc = &tls.Config{
			ServerName: name,
			MinVersion: tls.VersionTLS12,
			// Checked in VerifyConnection against the system roots or the fingerprint of
			// the connection, so that the message can name the fingerprint; never skipped.
			InsecureSkipVerify: true, //nolint:gosec // see VerifyConnection
			VerifyConnection:   verifyCertificate(addr, name, pins),
			// Data connections resume the TLS session of the control connection, as many
			// servers require.
			ClientSessionCache: tls.NewLRUClientSessionCache(1),
		}
		if cfg.tls == "explicit" {
			opts = append(opts, ftp.DialWithExplicitTLS(tc))
		} else {
			opts = append(opts, ftp.DialWithTLS(tc))
		}
	}
	opts = append(opts, ftp.DialWithDialFunc(f.dialFunc(ctx, timeout, tc, cfg.tls == "explicit")))
	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// Neither the greeting nor the sign-in have a context: closing the connections ends
	// them when the time is up or the run is cancelled.
	stop := context.AfterFunc(dctx, f.Close)
	signingIn := false
	f.c, err = ftp.Dial(addr, opts...)
	if err == nil {
		signingIn = true
		err = f.c.Login(cfg.user, cfg.password)
	}
	if !stop() {
		f.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%s did not answer within %s", addr, timeout)
	}
	if err != nil {
		f.Close()
		return nil, f.dialError(addr, cfg, signingIn, err)
	}
	f.mlst = f.c.IsTimePreciseInList() // the server knows MLST and MLSD
	return &session{addr: addr, user: cfg.user, fs: f}, nil
}

// dialError explains why connecting or signing in failed, without the password.
func (f *ftpFS) dialError(addr string, cfg ftpConfig, signingIn bool, err error) error {
	if ce, ok := errors.AsType[*certError](err); ok {
		return ce
	}
	te, reply := errors.AsType[*textproto.Error](err)
	switch {
	case signingIn && (reply && (te.Code == ftp.StatusNotLoggedIn || te.Code == ftp.StatusInvalidCredentials) || !reply && !f.Lost(err)):
		return &signInError{fmt.Sprintf("sign-in to %s as %s failed: wrong user or password (the server says: %s)", addr, cfg.user, strings.TrimSpace(err.Error()))}
	case !signingIn && cfg.tls == "explicit" && reply && te.Code >= 500:
		return fmt.Errorf("%s does not offer TLS with AUTH TLS (the server says: %w); check tls of the connection", addr, err)
	}
	return fmt.Errorf("cannot connect to %s: %w", addr, plainNet(err))
}

// certPins reads fingerprint: SHA256 fingerprints of certificates as hex digits, with or
// without colons, separated by spaces or commas.
func certPins(text string) ([][]byte, error) {
	var out [][]byte
	for _, f := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		raw, err := hex.DecodeString(strings.ReplaceAll(strings.TrimPrefix(strings.ToUpper(f), "SHA256:"), ":", ""))
		if err != nil || len(raw) != sha256.Size {
			return nil, fmt.Errorf("fingerprint: %q is not the SHA256 fingerprint of a certificate like AB:CD:…:EF (64 hex digits)", f)
		}
		out = append(out, raw)
	}
	return out, nil
}

// verifyCertificate accepts the certificate of the server if its fingerprint is pinned, or
// without pins if the system (rootCAs) trusts it for name. The messages name the
// fingerprint to check, as for hostKey of sftp.
func verifyCertificate(addr, name string, pins [][]byte) func(tls.ConnectionState) error {
	return func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return &certError{fmt.Sprintf("%s sent no certificate", addr)}
		}
		leaf := cs.PeerCertificates[0]
		sum := sha256.Sum256(leaf.Raw)
		hexes := make([]string, len(sum))
		for i, b := range sum {
			hexes[i] = fmt.Sprintf("%02X", b)
		}
		got := strings.Join(hexes, ":")
		if len(pins) > 0 {
			if slices.ContainsFunc(pins, func(p []byte) bool { return bytes.Equal(p, sum[:]) }) {
				return nil
			}
			return &certError{fmt.Sprintf("the certificate of %s (SHA256 %s) does not match fingerprint of the connection; the server may have been replaced or someone is in between: check the fingerprint with the administrator of the server before changing fingerprint", addr, got)}
		}
		opts := x509.VerifyOptions{DNSName: name, Roots: rootCAs, Intermediates: x509.NewCertPool()}
		for _, c := range cs.PeerCertificates[1:] {
			opts.Intermediates.AddCert(c)
		}
		if _, err := leaf.Verify(opts); err != nil {
			return &certError{fmt.Sprintf("the certificate of %s is not trusted (%s); if the server has a certificate of its own, make sure with the administrator of the server that its SHA256 fingerprint is %s, then set it as fingerprint of the connection", addr, strings.TrimPrefix(err.Error(), "x509: "), got)}
		}
		return nil
	}
}

// certError is a refused certificate.
type certError struct{ msg string }

func (e *certError) Error() string { return e.msg }

// ftpFS is a server of an ftps or ftp connection. FTP has a control connection and one
// connection per transfer or listing; Close closes all of them.
type ftpFS struct {
	c    *ftp.ServerConn
	mlst bool // the server answers MLST; otherwise Stat lists the folder

	mu     sync.Mutex
	conns  map[*trackedConn]struct{}
	dialed int
	closed bool
}

// trackedConn is a connection of an ftpFS that forgets itself when closed.
type trackedConn struct {
	net.Conn
	f *ftpFS
}

func (c *trackedConn) Close() error {
	c.f.mu.Lock()
	delete(c.f.conns, c)
	c.f.mu.Unlock()
	return c.Conn.Close()
}

// dialFunc opens the connections for the library: the control connection first, then one
// per transfer. With TLS all start it at once but an explicit control connection, which the
// library upgrades after AUTH TLS.
func (f *ftpFS) dialFunc(ctx context.Context, timeout time.Duration, tc *tls.Config, explicit bool) func(network, addr string) (net.Conn, error) {
	return func(network, addr string) (net.Conn, error) {
		raw, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		conn := &trackedConn{Conn: raw, f: f}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.closed {
			_ = raw.Close()
			return nil, net.ErrClosed
		}
		f.conns[conn] = struct{}{}
		f.dialed++
		if tc == nil || explicit && f.dialed == 1 {
			return conn, nil
		}
		return tls.Client(conn, tc), nil
	}
}

// Close closes all connections; calls on the server fail from then on.
func (f *ftpFS) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	for c := range f.conns {
		_ = c.Conn.Close()
	}
	clear(f.conns)
}

// Lost reports whether err means that the connection dropped.
func (f *ftpFS) Lost(err error) bool {
	if te, ok := errors.AsType[*textproto.Error](err); ok {
		return te.Code == ftp.StatusNotAvailable
	}
	_, isNet := errors.AsType[net.Error](err)
	return isNet || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed)
}

// replyCode returns the code of an error reply of the server; 0 for other errors.
func replyCode(err error) int {
	if te, ok := errors.AsType[*textproto.Error](err); ok {
		return te.Code
	}
	return 0
}

// Stat returns an entry with MLST, or from the listing of its folder where the server has
// no MLST (then links count as links: FTP does not tell what they point to).
func (f *ftpFS) Stat(p string) (fs.FileInfo, error) {
	if f.mlst {
		e, err := f.c.GetEntry(p)
		switch replyCode(err) {
		case 0:
			if err != nil {
				return nil, err
			}
			return entryInfo(path.Base(p), e), nil
		case ftp.StatusFileUnavailable, ftp.StatusFileActionIgnored:
			return nil, fs.ErrNotExist
		case ftp.StatusBadCommand, ftp.StatusNotImplemented:
			f.mlst = false
		default:
			return nil, err
		}
	}
	clean := path.Clean(p)
	if clean == "." || clean == "/" {
		return ftpInfo{name: clean, mode: fs.ModeDir | 0o755}, nil // the folder after the sign-in, the root
	}
	dir, name := path.Split(clean)
	if dir == "" {
		dir = "."
	}
	entries, err := f.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Name() == name {
			return e, nil
		}
	}
	return nil, fs.ErrNotExist
}

// Lstat is Stat: FTP shows links, if at all, only in a listing.
func (f *ftpFS) Lstat(p string) (fs.FileInfo, error) { return f.Stat(p) }

// ReadDir lists a folder with MLSD, or LIST where the server has no MLSD (the library reads
// the usual formats of Unix and Windows servers). The folder itself and its parent (. and
// ..) are left out.
func (f *ftpFS) ReadDir(dir string) ([]fs.FileInfo, error) {
	entries, err := f.c.List(dir)
	if code := replyCode(err); code == ftp.StatusFileUnavailable || code == ftp.StatusFileActionIgnored {
		return nil, fs.ErrNotExist
	}
	if err != nil {
		return nil, err
	}
	out := make([]fs.FileInfo, 0, len(entries))
	for _, e := range entries {
		if e.Name != "." && e.Name != ".." {
			out = append(out, entryInfo(e.Name, e))
		}
	}
	return out, nil
}

// MkdirAll creates a folder with its missing parents.
func (f *ftpFS) MkdirAll(dir string) error {
	if dir == "" || dir == "." || dir == "/" {
		return nil
	}
	info, err := f.Stat(dir)
	switch {
	case err == nil && info.IsDir():
		return nil
	case err == nil:
		return fmt.Errorf("%q is a file", dir)
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	if err := f.MkdirAll(path.Dir(dir)); err != nil {
		return err
	}
	if err := f.c.MakeDir(dir); err != nil {
		if info, serr := f.Stat(dir); serr == nil && info.IsDir() {
			return nil // created in the meantime
		}
		return err
	}
	return nil
}

func (f *ftpFS) Create(p string, r io.Reader) error { return f.c.Stor(p, r) }

func (f *ftpFS) Retrieve(p string, w io.Writer) error {
	resp, err := f.c.Retr(p)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, resp)
	if cerr := resp.Close(); err == nil {
		err = cerr
	}
	return err
}

// Chtimes sets the modification time with MFMT where the server has it, else nothing.
func (f *ftpFS) Chtimes(p string, t time.Time) {
	if f.c.IsSetTimeSupported() {
		_ = f.c.SetTime(p, t)
	}
}

// Rename renames with RNFR and RNTO; servers differ when the target exists, so an existing
// file is removed first if replace is set.
func (f *ftpFS) Rename(from, to string, replace bool) error {
	if replace {
		if err := f.c.Delete(to); err != nil && replyCode(err) != ftp.StatusFileUnavailable {
			return err
		}
	}
	return f.c.Rename(from, to)
}

func (f *ftpFS) Remove(p string) error    { return f.c.Delete(p) }
func (f *ftpFS) RemoveDir(p string) error { return f.c.RemoveDir(p) }
func (f *ftpFS) Getwd() (string, error)   { return f.c.CurrentDir() }

// ftpInfo is an entry of a listing.
type ftpInfo struct {
	name string
	size int64
	mod  time.Time
	mode fs.FileMode
}

func entryInfo(name string, e *ftp.Entry) ftpInfo {
	mode := fs.FileMode(0o644)
	switch e.Type {
	case ftp.EntryTypeFolder:
		mode = fs.ModeDir | 0o755
	case ftp.EntryTypeLink:
		mode = fs.ModeSymlink | 0o777
	}
	return ftpInfo{name: name, size: int64(min(e.Size, math.MaxInt64)), mod: e.Time, mode: mode}
}

func (i ftpInfo) Name() string       { return i.name }
func (i ftpInfo) Size() int64        { return i.size }
func (i ftpInfo) Mode() fs.FileMode  { return i.mode }
func (i ftpInfo) ModTime() time.Time { return i.mod }
func (i ftpInfo) IsDir() bool        { return i.mode.IsDir() }
func (i ftpInfo) Sys() any           { return nil }
