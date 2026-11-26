package transfer

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/spf13/afero"

	"github.com/runnelhq/runnel/internal/connections"
)

// withoutMLST switches off MLST, MLSD and MFMT: listings with LIST, no modification times.
func withoutMLST(s *ftpserver.Settings) {
	s.DisableMLST, s.DisableMLSD, s.DisableMFMT = true, true, true
}

// implicitTLS makes an ftps server speak TLS from the start.
func implicitTLS(s *ftpserver.Settings) { s.TLSRequired = ftpserver.ImplicitEncryption }

// newFTPServer starts an FTP server (ftpserverlib) on 127.0.0.1 for the tests: typ ftps
// with a self-signed certificate for 127.0.0.1 and localhost (TLS required, explicit unless
// implicitTLS), or ftp without TLS. The connection pins the certificate.
func newFTPServer(t testing.TB, typ string, options ...func(*ftpserver.Settings)) *testServer {
	t.Helper()
	srv := &testServer{typ: typ, root: t.TempDir(), fields: map[string]any{}}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, srv.port, _ = net.SplitHostPort(l.Addr().String())
	handlers := &sync.WaitGroup{}
	var listener net.Listener = &trackingListener{Listener: l, srv: srv, handlers: handlers}
	settings := &ftpserver.Settings{Listener: listener, DisableActiveMode: true, Banner: "runnel test server"}
	driver := &ftpDriver{root: srv.root, settings: settings}
	if typ == connections.TypeFTPS {
		cert := testCertificate(t)
		srv.cert, _ = x509.ParseCertificate(cert.Certificate[0])
		srv.fingerprint = certText(srv.cert)
		srv.fields["fingerprint"] = srv.fingerprint
		driver.tls = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		settings.TLSRequired = ftpserver.MandatoryEncryption
	} else {
		srv.fields["insecure"] = true
	}
	for _, o := range options {
		o(settings)
	}
	srv.times = !settings.DisableMFMT && !settings.DisableMLST
	if settings.TLSRequired == ftpserver.ImplicitEncryption {
		settings.Listener = tls.NewListener(listener, driver.tls)
		srv.fields["tls"] = "implicit"
	}
	server := ftpserver.NewFtpServer(driver)
	if err := server.Listen(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve()
	}()
	t.Cleanup(func() {
		_ = server.Stop()
		<-done
		srv.drop()
		handlers.Wait() // the files the server opened are closed (Windows)
	})
	return srv
}

// ftpDriver serves the files of root to the user runnel with the test password.
type ftpDriver struct {
	root     string
	settings *ftpserver.Settings
	tls      *tls.Config
}

func (d *ftpDriver) GetSettings() (*ftpserver.Settings, error) { return d.settings, nil }

func (d *ftpDriver) ClientConnected(ftpserver.ClientContext) (string, error) {
	return "runnel test server", nil
}

func (d *ftpDriver) ClientDisconnected(ftpserver.ClientContext) {}

func (d *ftpDriver) AuthUser(_ ftpserver.ClientContext, user, pass string) (ftpserver.ClientDriver, error) {
	if user != "runnel" || pass != testPassword {
		return nil, errors.New("wrong user or password")
	}
	return afero.NewBasePathFs(afero.NewOsFs(), d.root), nil
}

func (d *ftpDriver) GetTLSConfig() (*tls.Config, error) {
	if d.tls == nil {
		return nil, errors.New("no TLS")
	}
	return d.tls, nil
}

// trackingListener counts the control connections and can drop them (testServer.drop);
// handlers ends when the server has closed all of them.
type trackingListener struct {
	net.Listener
	srv      *testServer
	handlers *sync.WaitGroup
}

func (l *trackingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.srv.mu.Lock()
	l.srv.conns = append(l.srv.conns, c)
	l.srv.accepted++
	l.srv.mu.Unlock()
	l.handlers.Add(1)
	return &handlerConn{Conn: c, done: l.handlers.Done}, nil
}

// handlerConn reports when the server closes it.
type handlerConn struct {
	net.Conn
	once sync.Once
	done func()
}

func (c *handlerConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.done)
	return err
}

// testCertificate returns a self-signed certificate for 127.0.0.1 and localhost.
func testCertificate(t testing.TB) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "runnel test server"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// certText returns the SHA256 fingerprint of a certificate as AB:CD:…, as Runnel names it.
func certText(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}
