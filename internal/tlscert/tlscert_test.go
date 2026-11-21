package tlscert

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	dns = []string{"localhost", "srv01"}
	ips = []net.IP{net.IPv4(127, 0, 0, 1), net.IPv4(192, 0, 2, 10)}
)

func TestSelfSigned(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	now := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
	c, created, err := SelfSigned(dir, dns, ips, now)
	if err != nil || !created {
		t.Fatalf("created %v: %v", created, err)
	}
	if c.Leaf.Subject.CommonName != "srv01" || !c.Leaf.NotAfter.Equal(now.Add(Validity).Truncate(time.Second)) {
		t.Errorf("leaf %v %v", c.Leaf.Subject, c.Leaf.NotAfter)
	}
	// A client that trusts the certificate file reaches the server under its names.
	pool := x509.NewCertPool()
	data, _ := os.ReadFile(filepath.Join(dir, CertFile))
	if !pool.AppendCertsFromPEM(data) {
		t.Fatal("certificate file")
	}
	for _, name := range []string{"srv01", "192.0.2.10", "localhost"} {
		if _, err := c.Leaf.Verify(x509.VerifyOptions{DNSName: name, Roots: pool, CurrentTime: now}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if info, err := os.Stat(filepath.Join(dir, KeyFile)); err != nil || (info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/') {
		t.Errorf("key file %v %v", info, err)
	}

	// Reused while it is valid; created anew shortly before its end or for a new name.
	again, created, err := SelfSigned(dir, dns, ips, now.Add(365*24*time.Hour))
	if err != nil || created || Fingerprint(again) != Fingerprint(c) {
		t.Errorf("reuse: created %v, %v", created, err)
	}
	if _, created, _ := SelfSigned(dir, dns, ips, now.Add(Validity-10*24*time.Hour)); !created {
		t.Error("not renewed before its end")
	}
	if _, created, _ := SelfSigned(dir, []string{"localhost", "srv02"}, ips, now); !created {
		t.Error("not renewed for a new name")
	}
	if fp := Fingerprint(c); len(fp) != 95 || strings.Count(fp, ":") != 31 {
		t.Errorf("fingerprint %q", fp)
	}
	if d := Describe(c); !strings.Contains(d, "for localhost, srv01, 127.0.0.1, 192.0.2.10, valid until 2028-11-20, SHA-256 ") {
		t.Errorf("describe %q", d)
	}
}

// An own certificate is loaded from PEM files and loaded again after it was renewed.
func TestFiles(t *testing.T) {
	CheckEvery = 0
	defer func() { CheckEvery = time.Minute }()
	dir := t.TempDir()
	write := func(name string) {
		t.Helper()
		certPEM, keyPEM, err := generate([]string{"localhost", name}, nil, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(dir, "own.crt"), certPEM, 0o644)
		_ = os.WriteFile(filepath.Join(dir, "own.key"), keyPEM, 0o600)
		later := time.Now().Add(time.Duration(len(name)) * time.Minute)
		_ = os.Chtimes(filepath.Join(dir, "own.crt"), later, later)
	}
	write("first")
	f, err := LoadFiles(filepath.Join(dir, "own.crt"), filepath.Join(dir, "own.key"))
	if err != nil {
		t.Fatal(err)
	}
	var reloaded []string
	f.OnReload = func(c tls.Certificate, err error) {
		if err != nil {
			reloaded = append(reloaded, "error")
		} else {
			reloaded = append(reloaded, c.Leaf.Subject.CommonName)
		}
	}
	if f.Current().Leaf.Subject.CommonName != "first" {
		t.Error("first")
	}
	write("renewed")
	if cn := f.Current().Leaf.Subject.CommonName; cn != "renewed" || strings.Join(reloaded, ",") != "renewed" {
		t.Errorf("after renewal %s, reloaded %v", cn, reloaded)
	}
	// A broken file keeps the previous certificate.
	_ = os.WriteFile(filepath.Join(dir, "own.key"), []byte("broken"), 0o600)
	if cn := f.Current().Leaf.Subject.CommonName; cn != "renewed" || reloaded[len(reloaded)-1] != "error" {
		t.Errorf("broken: %s %v", cn, reloaded)
	}
	if _, err := LoadFiles(filepath.Join(dir, "none.crt"), filepath.Join(dir, "own.key")); err == nil || !strings.Contains(err.Error(), "certificate or key not found") {
		t.Errorf("missing: %v", err)
	}
	if f.Config().GetCertificate == nil || Static(f.Current()).MinVersion != tls.VersionTLS12 {
		t.Error("config")
	}
}
