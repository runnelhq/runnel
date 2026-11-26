package transfer

import (
	"context"
	"errors"
	"maps"
	"net"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
)

func resolved(with map[string]any) connection.Resolved {
	return connection.Resolved{Name: "sftp", Type: connections.TypeSFTP, With: with}
}

// Check signs in and lists the folder without writing (Test connection, ADR 0022).
func TestCheck(t *testing.T) {
	srv := newTestServer(t)
	writeFile(t, srv.root, "a.csv", "1")
	writeFile(t, srv.root, "b.csv", "2")
	msg, err := Check(context.Background(), resolved(srv.conn()))
	if err != nil || !strings.HasPrefix(msg, "signed in to 127.0.0.1:"+srv.port+" as runnel; folder ") || !strings.HasSuffix(msg, " has 2 entries") {
		t.Fatalf("%q %v", msg, err)
	}
	if got := tempFiles(t, srv.root); len(got) != 0 {
		t.Errorf("files written: %v", got)
	}
}

// Without hostKey Runnel does not connect; the message names the fingerprint to check.
func TestHostKeyUnknown(t *testing.T) {
	srv := newTestServer(t)
	with := srv.conn()
	delete(with, "hostKey")
	_, err := Check(context.Background(), resolved(with))
	if err == nil || !strings.Contains(err.Error(), "is not known") || !strings.Contains(err.Error(), srv.fingerprint+" (ssh-ed25519)") {
		t.Fatalf("%v", err)
	}
	if strings.Contains(err.Error(), testPassword) {
		t.Error("the password is in the message")
	}
	// An action fails the same way.
	_, _, err = execute(context.Background(), t, listDefinition, with, map[string]any{"folder": "."})
	if err == nil || !strings.Contains(err.Error(), srv.fingerprint) {
		t.Errorf("%v", err)
	}
	// validate warns about the missing hostKey.
	if w := connections.Builtin().Warnings(connections.TypeSFTP, with); !strings.Contains(w["hostKey"], "Test connection shows the fingerprint") {
		t.Errorf("%v", w)
	}
}

func TestHostKeyMismatch(t *testing.T) {
	srv := newTestServer(t)
	other := newTestServer(t)
	with := srv.conn()
	with["hostKey"] = other.fingerprint
	_, err := Check(context.Background(), resolved(with))
	if err == nil || !strings.Contains(err.Error(), "does not match hostKey") || !strings.Contains(err.Error(), srv.fingerprint) {
		t.Fatalf("%v", err)
	}
	// Several fingerprints: one of them must match; without "SHA256:" and with padding too.
	with["hostKey"] = other.fingerprint + ", " + strings.TrimPrefix(srv.fingerprint, "SHA256:") + "="
	if _, err := Check(context.Background(), resolved(with)); err != nil {
		t.Errorf("%v", err)
	}
	for _, bad := range []string{"MD5:16:27:ac:a5:76:28:2d:36:63:1b:56:4d:eb:df:a6:48", "SHA256:short"} {
		with["hostKey"] = bad
		if _, err := Check(context.Background(), resolved(with)); err == nil || !strings.Contains(err.Error(), "is not a SHA256 fingerprint") {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

func TestSignIn(t *testing.T) {
	srv := newTestServer(t)
	base := srv.conn()
	delete(base, "password")
	with := func(fields map[string]any) map[string]any {
		out := maps.Clone(base)
		maps.Copy(out, fields)
		return out
	}
	protected := srv.keyPEM(t, "key-pass") // bcrypt: slow, made once
	ok := []map[string]any{
		{"privateKey": srv.keyPEM(t, "")},
		{"privateKey": protected, "passphrase": "key-pass"},
		{"privateKey": srv.keyPEM(t, ""), "passphrase": "not needed"},
		{"privateKey": srv.keyPEM(t, ""), "password": "wrong but the key is enough"},
	}
	for _, f := range ok {
		if _, err := Check(context.Background(), resolved(with(f))); err != nil {
			t.Errorf("%v: %v", f["passphrase"], err)
		}
	}
	fail := []struct {
		fields map[string]any
		want   string
	}{
		{map[string]any{"password": "wrong-pw"}, "sign-in to 127.0.0.1:" + srv.port + " as runnel failed"},
		{map[string]any{"password": testPassword, "user": "other"}, "as other failed"},
		{map[string]any{}, "no sign-in: set password or privateKey"},
		{map[string]any{"privateKey": protected}, "privateKey is protected by a passphrase; set passphrase"},
		{map[string]any{"privateKey": protected, "passphrase": "wrong-pass"}, "passphrase does not open privateKey"},
		{map[string]any{"privateKey": "-----BEGIN OPENSSH PRIVATE KEY-----\nc2VjcmV0LWtleS10ZXh0\n-----END OPENSSH PRIVATE KEY-----\n"}, "privateKey is not a private key in PEM format"},
		{map[string]any{"password": testPassword, "port": "70000"}, `port: "70000" is not a TCP port`},
		{map[string]any{"password": testPassword, "timeout": "soon"}, `timeout: "soon" is not a duration`},
		{map[string]any{"password": testPassword, "host": ""}, "host is empty"},
	}
	for _, c := range fail {
		_, err := Check(context.Background(), resolved(with(c.fields)))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: error %v, want %q", c.fields, err, c.want)
			continue
		}
		// Secrets never appear in messages.
		for _, secret := range []string{"wrong-pw", "wrong-pass", "key-pass", "c2VjcmV0LWtleS10ZXh0", "PRIVATE KEY-----\n"} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("%v: the message contains a secret: %v", c.fields, err)
			}
		}
	}
	// The wrong type of connection.
	if _, err := connect(context.Background(), connection.Resolved{Name: "mail", Type: connections.TypeSMTP, With: map[string]any{"host": "x"}}); err == nil || err.Error() != "connection mail has type smtp, not sftp" {
		t.Errorf("%v", err)
	}
}

// silentServer accepts connections and never answers.
func silentServer(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var conns []net.Conn
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			conns = append(conns, c)
		}
	}()
	t.Cleanup(func() {
		_ = l.Close()
		<-done
		for _, c := range conns {
			_ = c.Close()
		}
	})
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port
}

func TestConnectTimeoutAndCancel(t *testing.T) {
	port := silentServer(t)
	with := map[string]any{"host": "127.0.0.1", "port": port, "user": "runnel", "password": testPassword, "hostKey": "SHA256:ExampleFingerprintOfTheServerHostKeyAbcdefg", "timeout": "200ms"}
	start := time.Now()
	_, err := Check(context.Background(), resolved(with))
	if err == nil || !strings.Contains(err.Error(), "did not answer within 200ms") {
		t.Fatalf("%v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("took %s", time.Since(start))
	}
	// Cancelling the run ends the connection attempt.
	with["timeout"] = "1m"
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _, err = execute(ctx, t, listDefinition, with, map[string]any{"folder": "."})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("%v", err)
	}
	// Nobody listens.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	_, closed, _ := net.SplitHostPort(l.Addr().String())
	_ = l.Close()
	with["port"] = closed
	if _, err := Check(context.Background(), resolved(with)); err == nil || !strings.Contains(err.Error(), "cannot connect to 127.0.0.1:"+closed) {
		t.Errorf("%v", err)
	}
}

func TestLocalName(t *testing.T) {
	for name, want := range map[string]bool{"a.csv": true, "..a": true, "": false, ".": false, "..": false, `..\x`: false, "a/b": false, `a\b`: false} {
		if got := localName(name); got != want {
			t.Errorf("localName(%q) = %v", name, got)
		}
	}
	if runtime.GOOS == "windows" {
		for _, name := range []string{"C:x", "NUL", "con.txt"} {
			if localName(name) {
				t.Errorf("localName(%q) = true", name)
			}
		}
	}
}
