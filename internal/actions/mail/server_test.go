package mail

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/tlscert"
)

// testServer is a small SMTP server for the tests: it offers the extensions it is given
// and records what it receives.
type testServer struct {
	addr      string
	tls       *tls.Config
	implicit  bool     // TLS from the start (port 465)
	ext       []string // e.g. STARTTLS, AUTH PLAIN LOGIN, SIZE 100
	silent    bool     // never greets (timeouts)
	rejectRcp string   // recipient that is refused

	mu       sync.Mutex
	from     string
	rcpt     []string
	data     string
	user     string
	password string
	mech     string
	tlsUsed  bool
}

// certificate returns a TLS configuration for 127.0.0.1 and lets the client trust it.
func certificate(t *testing.T) *tls.Config {
	t.Helper()
	cert, _, err := tlscert.SelfSigned(t.TempDir(), []string{"localhost"}, []net.IP{net.IPv4(127, 0, 0, 1)}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	old := rootCAs
	rootCAs = pool
	t.Cleanup(func() { rootCAs = old })
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
}

func (s *testServer) start(t *testing.T) {
	t.Helper()
	var ln net.Listener
	var err error
	if s.implicit {
		ln, err = tls.Listen("tcp", "127.0.0.1:0", s.tls)
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatal(err)
	}
	s.addr = ln.Addr().String()
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
}

func (s *testServer) port() string {
	_, port, _ := net.SplitHostPort(s.addr)
	return port
}

func (s *testServer) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	if s.silent {
		_, _ = bufio.NewReader(conn).ReadString('\n')
		return
	}
	_, implicit := conn.(*tls.Conn)
	s.setTLS(implicit)
	tp := textproto.NewConn(conn)
	reply := func(format string, args ...any) { _ = tp.PrintfLine(format, args...) }
	reply("220 test ready")
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		cmd, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(cmd) {
		case "EHLO":
			lines := append([]string{"test"}, s.ext...)
			for i, l := range lines {
				sep := "-"
				if i == len(lines)-1 {
					sep = " "
				}
				reply("250%s%s", sep, l)
			}
		case "STARTTLS":
			reply("220 go ahead")
			tc := tls.Server(conn, s.tls)
			if tc.Handshake() != nil {
				return
			}
			conn = tc
			s.setTLS(true)
			tp = textproto.NewConn(tc)
		case "AUTH":
			s.auth(tp, arg, reply)
		case "MAIL":
			s.mu.Lock()
			s.from = strings.Trim(strings.TrimPrefix(arg, "FROM:"), "<>")
			s.mu.Unlock()
			reply("250 ok")
		case "RCPT":
			to := strings.Trim(strings.TrimPrefix(arg, "TO:"), "<>")
			if to == s.rejectRcp {
				reply("550 no such user")
				continue
			}
			s.mu.Lock()
			s.rcpt = append(s.rcpt, to)
			s.mu.Unlock()
			reply("250 ok")
		case "DATA":
			reply("354 go ahead")
			lines, err := tp.ReadDotLines()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.data = strings.Join(lines, "\r\n")
			s.mu.Unlock()
			reply("250 queued")
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 unknown")
		}
	}
}

func (s *testServer) auth(tp *textproto.Conn, arg string, reply func(string, ...any)) {
	mech, initial, _ := strings.Cut(arg, " ")
	var user, password string
	switch mech {
	case "PLAIN":
		raw, _ := base64.StdEncoding.DecodeString(initial)
		parts := strings.Split(string(raw), "\x00")
		if len(parts) == 3 {
			user, password = parts[1], parts[2]
		}
	case "XOAUTH2":
		raw, _ := base64.StdEncoding.DecodeString(initial)
		fields := strings.Split(string(raw), "\x01")
		user = strings.TrimPrefix(fields[0], "user=")
		if len(fields) > 1 {
			password = strings.TrimPrefix(fields[1], "auth=Bearer ")
		}
		if password != "secret-pw" {
			s.mu.Lock()
			s.mech, s.user, s.password = mech, user, password
			s.mu.Unlock()
			reply("334 %s", base64.StdEncoding.EncodeToString([]byte(`{"status":"401","schemes":"bearer"}`)))
			_, _ = tp.ReadLine()
			reply("535 5.7.3 Authentication unsuccessful")
			return
		}
	case "LOGIN":
		read := func(prompt string) string {
			reply("334 %s", base64.StdEncoding.EncodeToString([]byte(prompt)))
			line, _ := tp.ReadLine()
			raw, _ := base64.StdEncoding.DecodeString(line)
			return string(raw)
		}
		user, password = read("Username:"), read("Password:")
	}
	s.mu.Lock()
	s.mech, s.user, s.password = mech, user, password
	s.mu.Unlock()
	if password != "secret-pw" {
		reply("535 authentication failed")
		return
	}
	reply("235 ok")
}

func (s *testServer) setTLS(used bool) {
	s.mu.Lock()
	s.tlsUsed = used
	s.mu.Unlock()
}

func (s *testServer) received() (from string, rcpt []string, data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.from, s.rcpt, s.data
}

func (s *testServer) String() string { return fmt.Sprintf("test server %s", s.addr) }
