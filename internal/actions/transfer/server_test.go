package transfer

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
)

// testServer is an SSH server with SFTP on 127.0.0.1 for the tests: it serves the files
// of root and accepts the password "secret-pw" or the key of clientKey for the user runnel.
type testServer struct {
	root        string
	port        string
	fingerprint string
	clientKey   ed25519.PrivateKey

	mu       sync.Mutex
	conns    []net.Conn
	accepted int // connections so far
}

const testPassword = "secret-pw"

func newTestServer(t testing.TB) *testServer {
	t.Helper()
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostKey, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	clientPub, clientPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	authorized, err := ssh.NewPublicKey(clientPub)
	if err != nil {
		t.Fatal(err)
	}
	srv := &testServer{root: t.TempDir(), fingerprint: ssh.FingerprintSHA256(hostKey.PublicKey()), clientKey: clientPriv}
	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if c.User() == "runnel" && string(pw) == testPassword {
				return nil, nil
			}
			return nil, errors.New("wrong password")
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if c.User() == "runnel" && string(key.Marshal()) == string(authorized.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("unknown key")
		},
	}
	config.AddHostKey(hostKey)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, srv.port, _ = net.SplitHostPort(l.Addr().String())
	var wg sync.WaitGroup
	t.Cleanup(func() {
		_ = l.Close()
		srv.drop()
		wg.Wait()
	})
	wg.Go(func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			srv.mu.Lock()
			srv.conns = append(srv.conns, conn)
			srv.accepted++
			srv.mu.Unlock()
			wg.Go(func() { srv.serve(conn, config) })
		}
	})
	return srv
}

// drop closes all connections, as if the network failed.
func (srv *testServer) drop() {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	for _, c := range srv.conns {
		_ = c.Close()
	}
	srv.conns = nil
}

// connections returns the number of connections so far.
func (srv *testServer) connections() int {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return srv.accepted
}

// serve speaks SSH on one connection and starts SFTP on request.
func (srv *testServer) serve(conn net.Conn, config *ssh.ServerConfig) {
	defer func() { _ = conn.Close() }()
	sc, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	// SFTP runs in goroutines of their own; wait for them, so that the files they opened
	// are closed before the test removes its folders (Windows).
	var sessions sync.WaitGroup
	defer func() {
		_ = sc.Close()
		sessions.Wait()
	}()
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "no")
			continue
		}
		ch, requests, err := nc.Accept()
		if err != nil {
			return
		}
		sessions.Go(func() {
			for req := range requests {
				ok := req.Type == "subsystem" && len(req.Payload) > 4 && string(req.Payload[4:]) == "sftp"
				_ = req.Reply(ok, nil)
				if ok {
					server, err := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(filepath.ToSlash(srv.root)))
					if err == nil {
						_ = server.Serve()
						_ = server.Close()
					}
					return
				}
			}
		})
	}
}

// conn returns the fields of a connection to the server with the password.
func (srv *testServer) conn() map[string]any {
	return map[string]any{"host": "127.0.0.1", "port": srv.port, "user": "runnel", "password": testPassword, "hostKey": srv.fingerprint}
}

// keyPEM returns the client key in the OpenSSH format, protected if passphrase is set.
func (srv *testServer) keyPEM(t testing.TB, passphrase string) string {
	t.Helper()
	var block *pem.Block
	var err error
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(srv.clientKey, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(srv.clientKey, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(block))
}

// writeFile writes a file below dir with its folders.
func writeFile(t testing.TB, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// read returns the content of a file below dir; "" if it is missing.
func read(t testing.TB, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return string(data)
}

// execute runs an action with the connection "sftp" of the given fields.
func execute(ctx context.Context, t testing.TB, def action.Definition, conn, with map[string]any) (action.Result, []string, error) {
	t.Helper()
	with["connection"] = "sftp"
	a, err := def.New(with)
	if err != nil {
		return action.Result{}, nil, err
	}
	var logs []string
	ec := &action.ExecContext{
		Log: func(_ slog.Level, msg string) { logs = append(logs, msg) },
		Connection: func(_ context.Context, name string) (connection.Resolved, error) {
			if name != "sftp" {
				return connection.Resolved{}, errors.New("unknown connection")
			}
			return connection.Resolved{Name: name, Type: connections.TypeSFTP, With: conn}, nil
		},
	}
	res, err := a.Execute(ctx, ec)
	return res, logs, err
}

// tempFiles returns the names of temporary files left in a folder and its subfolders.
func tempFiles(t testing.TB, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(p) == ".tmp" {
			out = append(out, p)
		}
		return nil
	})
	return out
}
