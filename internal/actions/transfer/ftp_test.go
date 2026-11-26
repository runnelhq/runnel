package transfer

import (
	"context"
	"crypto/x509"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/connections"
)

// with returns the fields of a connection to srv with some changed; nil removes one.
func with(srv *testServer, fields map[string]any) map[string]any {
	out := srv.conn()
	for k, v := range fields {
		if v == nil {
			delete(out, k)
		} else {
			out[k] = v
		}
	}
	return out
}

// Check signs in with explicit and implicit TLS and without TLS, and lists the folder.
func TestFTPCheck(t *testing.T) {
	servers := map[string]*testServer{
		"explicit": newFTPServer(t, connections.TypeFTPS),
		"implicit": newFTPServer(t, connections.TypeFTPS, implicitTLS),
		"ftp":      newFTPServer(t, connections.TypeFTP),
		"list":     newFTPServer(t, connections.TypeFTPS, withoutMLST),
	}
	for name, srv := range servers {
		writeFile(t, srv.root, "a.csv", "1")
		writeFile(t, srv.root, "sub/b.csv", "2")
		msg, err := Check(context.Background(), resolved(srv.conn()))
		if err != nil || msg != "signed in to 127.0.0.1:"+srv.port+" as runnel; folder / has 2 entries" {
			t.Errorf("%s: %q %v", name, msg, err)
		}
		if got := tempFiles(t, srv.root); len(got) != 0 {
			t.Errorf("%s: files written: %v", name, got)
		}
	}
}

// ftp needs insecure: true; validate warns either way.
func TestFTPInsecure(t *testing.T) {
	srv := newFTPServer(t, connections.TypeFTP)
	for _, v := range []any{nil, false} {
		w := with(srv, map[string]any{"insecure": v})
		_, err := Check(context.Background(), resolved(w))
		if err == nil || !strings.Contains(err.Error(), "connection sftp: ftp sends the password and all data unencrypted; use type ftps, or set insecure: true") {
			t.Errorf("%v: %v", v, err)
		}
		delete(w, "type")
		if got := connections.Builtin().Warnings(connections.TypeFTP, w); !strings.Contains(got["insecure"], "without insecure: true runs cannot connect") {
			t.Errorf("%v: %v", v, got)
		}
	}
	if srv.connections() != 0 {
		t.Error("connected without insecure")
	}
	w := srv.conn()
	delete(w, "type")
	if got := connections.Builtin().Warnings(connections.TypeFTP, w); !strings.Contains(got["insecure"], "the password and all data travel unencrypted") {
		t.Errorf("%v", got)
	}
	if got := connections.Builtin().Warnings(connections.TypeFTPS, map[string]any{"host": "h"}); len(got) != 0 {
		t.Errorf("ftps: %v", got)
	}
}

func TestFTPSignIn(t *testing.T) {
	for _, srv := range []*testServer{newFTPServer(t, connections.TypeFTPS), newFTPServer(t, connections.TypeFTP)} {
		for _, fields := range []map[string]any{{"password": "wrong-pw"}, {"user": "other"}} {
			_, err := Check(context.Background(), resolved(with(srv, fields)))
			if err == nil || !strings.Contains(err.Error(), "sign-in to 127.0.0.1:"+srv.port+" as ") || !strings.Contains(err.Error(), "failed: wrong user or password") || !refused(err) {
				t.Errorf("%s %v: %v", srv.typ, fields, err)
				continue
			}
			if strings.Contains(err.Error(), "wrong-pw") || strings.Contains(err.Error(), testPassword) {
				t.Errorf("the message contains the password: %v", err)
			}
		}
		// transfer.wait ends at once instead of trying again.
		start := time.Now()
		_, _, err := execute(context.Background(), t, waitDefinition, with(srv, map[string]any{"password": "wrong-pw"}), map[string]any{"path": "a", "interval": "10ms"})
		if err == nil || time.Since(start) > 5*time.Second {
			t.Errorf("wait: %v", err)
		}
	}
	// Errors of the fields.
	srv := newFTPServer(t, connections.TypeFTPS)
	fail := []struct {
		fields map[string]any
		want   string
	}{
		{map[string]any{"tls": "starttls"}, `tls: "starttls" is not explicit or implicit`},
		{map[string]any{"port": "0"}, `port: "0" is not a TCP port`},
		{map[string]any{"timeout": "later"}, `timeout: "later" is not a duration`},
		{map[string]any{"fingerprint": "AB:CD"}, `fingerprint: "AB:CD" is not the SHA256 fingerprint of a certificate`},
		{map[string]any{"insecure": true}, `unknown field "insecure"`},
	}
	for _, c := range fail {
		if _, err := Check(context.Background(), resolved(with(srv, c.fields))); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.fields, err, c.want)
		}
	}
}

// The certificate is checked against the system (here: rootCAs) or the fingerprint of the
// connection; the messages name the fingerprint of the server.
func TestFTPCertificate(t *testing.T) {
	for _, srv := range []*testServer{newFTPServer(t, connections.TypeFTPS), newFTPServer(t, connections.TypeFTPS, implicitTLS)} {
		mode := srv.fields["tls"]
		_, err := Check(context.Background(), resolved(with(srv, map[string]any{"fingerprint": nil})))
		if err == nil || !strings.Contains(err.Error(), "is not trusted (certificate signed by unknown authority)") || !strings.Contains(err.Error(), "its SHA256 fingerprint is "+srv.fingerprint) || !refused(err) {
			t.Errorf("%v: %v", mode, err)
		}
		other := newFTPServer(t, connections.TypeFTPS)
		_, err = Check(context.Background(), resolved(with(srv, map[string]any{"fingerprint": other.fingerprint})))
		if err == nil || !strings.Contains(err.Error(), "(SHA256 "+srv.fingerprint+") does not match fingerprint of the connection") {
			t.Errorf("%v: %v", mode, err)
		}
		// Several, lower case, without colons, with SHA256: in front.
		pin := other.fingerprint + ", SHA256:" + strings.ToLower(strings.ReplaceAll(srv.fingerprint, ":", ""))
		if _, err := Check(context.Background(), resolved(with(srv, map[string]any{"fingerprint": pin}))); err != nil {
			t.Errorf("%v: %v", mode, err)
		}
		if srv.connections() < 3 {
			t.Errorf("%v: %d connections", mode, srv.connections())
		}
	}
	// A certificate the system trusts, for the name of the server.
	srv := newFTPServer(t, connections.TypeFTPS)
	rootCAs = x509.NewCertPool()
	rootCAs.AddCert(srv.cert)
	t.Cleanup(func() { rootCAs = nil })
	for _, fields := range []map[string]any{{"fingerprint": nil}, {"fingerprint": nil, "host": "localhost"}, {"fingerprint": nil, "serverName": "localhost"}} {
		if _, err := Check(context.Background(), resolved(with(srv, fields))); err != nil {
			t.Errorf("%v: %v", fields, err)
		}
	}
	_, err := Check(context.Background(), resolved(with(srv, map[string]any{"fingerprint": nil, "serverName": "files.example.com"})))
	if err == nil || !strings.Contains(err.Error(), "is not trusted (certificate is valid for localhost, not files.example.com)") {
		t.Errorf("%v", err)
	}
	// Without TLS at the server, ftps does not connect.
	plain := newFTPServer(t, connections.TypeFTP)
	fields := maps.Clone(srv.fields)
	fields["insecure"] = nil
	fields["port"] = plain.port
	if _, err := Check(context.Background(), resolved(with(srv, fields))); err == nil || !strings.Contains(err.Error(), "does not offer TLS with AUTH TLS") {
		t.Errorf("%v", err)
	}
	fields["tls"] = "implicit"
	if _, err := Check(context.Background(), resolved(with(srv, fields))); err == nil || !strings.Contains(err.Error(), "cannot connect to 127.0.0.1:"+plain.port) {
		t.Errorf("%v", err)
	}
}

// A server that does not answer ends the attempt after timeout of the connection;
// cancelling the run ends it at once.
func TestFTPConnectTimeoutAndCancel(t *testing.T) {
	port := silentServer(t)
	for _, fields := range []map[string]any{
		{"type": "ftps", "fingerprint": "AB:" + strings.Repeat("00:", 30) + "CD"},
		{"type": "ftps", "tls": "implicit"},
		{"type": "ftp", "insecure": true},
	} {
		w := map[string]any{"host": "127.0.0.1", "port": port, "user": "runnel", "password": testPassword, "timeout": "200ms"}
		maps.Copy(w, fields)
		start := time.Now()
		_, err := Check(context.Background(), resolved(w))
		if err == nil || !strings.Contains(err.Error(), "did not answer within 200ms") {
			t.Errorf("%v: %v", fields, err)
		}
		if time.Since(start) > 5*time.Second {
			t.Errorf("took %s", time.Since(start))
		}
		w["timeout"] = "1m"
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		_, _, err = execute(ctx, t, listDefinition, w, map[string]any{"folder": "."})
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%v: %v", fields, err)
		}
	}
}

// A transfer that takes longer than timeout closes all connections, including the one of
// the data.
func TestFTPTransferTimeout(t *testing.T) {
	srv := newFTPServer(t, connections.TypeFTPS)
	writeFile(t, srv.root, "big.bin", strings.Repeat("x", 8<<20))
	s, err := connect(context.Background(), resolved(srv.conn()))
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	info, err := s.stat("big.bin")
	if err != nil || info == nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = s.within(context.Background(), 50*time.Millisecond, "the download of big.bin", func() error {
		return s.fs.Retrieve("big.bin", writerFunc(func(p []byte) (int, error) {
			time.Sleep(20 * time.Millisecond) // a slow disk
			return len(p), nil
		}))
	})
	if err == nil || !strings.Contains(err.Error(), "did not finish within 50ms (timeout)") || time.Since(start) > 5*time.Second {
		t.Errorf("%v after %s", err, time.Since(start))
	}
	if !s.closed() {
		t.Error("the session is still open")
	}
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestCertPins(t *testing.T) {
	hexes := strings.Repeat("ab", 32)
	pins, err := certPins(" SHA256:" + hexes + ",\n" + strings.ToUpper(hexes))
	if err != nil || len(pins) != 2 || pins[0][0] != 0xab {
		t.Errorf("%v %v", pins, err)
	}
	for _, bad := range []string{"SHA256:q83vEi3Kq5dcd5PNrjHwqTUlQ1oHVZK9bQVjNgUAA8A", hexes + "ab", "zz"} {
		if _, err := certPins(bad); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
	if !refused(&certError{"x"}) {
		t.Error("a refused certificate does not end transfer.wait")
	}
}
