// Package tlscert provides the certificates of the listener of http triggers (roadmap
// S40a): a self-signed certificate that is created once and then reused, or an own
// certificate from PEM files that is picked up again when the files change.
package tlscert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Files of the self-signed certificate in its folder.
const (
	CertFile = "triggers.crt"
	KeyFile  = "triggers.key"
)

// Validity of a self-signed certificate; it is created anew this long before it ends.
const (
	Validity = 2 * 365 * 24 * time.Hour
	renewal  = 30 * 24 * time.Hour
)

// Names returns the names a self-signed certificate is made for: localhost, the name
// of the computer and the addresses of its network interfaces.
func Names() (dns []string, ips []net.IP) {
	dns = []string{"localhost"}
	if host, err := os.Hostname(); err == nil && host != "" {
		dns = append(dns, strings.ToLower(host))
	}
	ips = []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && !n.IP.IsLinkLocalUnicast() {
				ips = append(ips, n.IP)
			}
		}
	}
	return dns, ips
}

// SelfSigned returns the self-signed certificate in dir, creating it if there is none,
// it ends within 30 days or it does not cover dns and ips (e.g. after renaming the
// computer). created tells whether it was created now.
func SelfSigned(dir string, dns []string, ips []net.IP, now time.Time) (cert tls.Certificate, created bool, err error) {
	certPath, keyPath := filepath.Join(dir, CertFile), filepath.Join(dir, KeyFile)
	if c, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil && covers(c.Leaf, dns, ips, now) {
		return c, false, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return tls.Certificate{}, false, fmt.Errorf("cannot create the folder for the certificate: %w", err)
	}
	certPEM, keyPEM, err := generate(dns, ips, now)
	if err != nil {
		return tls.Certificate{}, false, err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, false, fmt.Errorf("cannot write the key of the certificate: %w", err)
	}
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return tls.Certificate{}, false, fmt.Errorf("cannot write the certificate: %w", err)
	}
	c, err := tls.X509KeyPair(certPEM, keyPEM)
	return c, true, err
}

// covers reports whether a certificate is valid for 30 more days and made for all names.
func covers(leaf *x509.Certificate, dns []string, ips []net.IP, now time.Time) bool {
	if leaf == nil || now.Add(renewal).After(leaf.NotAfter) {
		return false
	}
	for _, d := range dns {
		if !slices.Contains(leaf.DNSNames, d) {
			return false
		}
	}
	for _, ip := range ips {
		if !slices.ContainsFunc(leaf.IPAddresses, ip.Equal) {
			return false
		}
	}
	return true
}

func generate(dns []string, ips []net.IP, now time.Time) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, err
	}
	name := "runnel"
	if len(dns) > 1 {
		name = dns[1]
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: name, Organization: []string{"Runnel (self-signed)"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(Validity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true, // so that callers can trust it directly
		DNSNames:              dns,
		IPAddresses:           ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), nil
}

// Fingerprint returns the SHA-256 fingerprint of a certificate, e.g. "AB:12:…".
func Fingerprint(c tls.Certificate) string {
	if len(c.Certificate) == 0 {
		return ""
	}
	sum := sha256.Sum256(c.Certificate[0])
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}

// Describe tells about a certificate for the log: names, end and fingerprint.
func Describe(c tls.Certificate) string {
	if c.Leaf == nil {
		return ""
	}
	names := slices.Clone(c.Leaf.DNSNames)
	for _, ip := range c.Leaf.IPAddresses {
		names = append(names, ip.String())
	}
	return fmt.Sprintf("for %s, valid until %s, SHA-256 %s", strings.Join(names, ", "), c.Leaf.NotAfter.Format("2006-01-02"), Fingerprint(c))
}

// Files serves an own certificate from PEM files; changed files are loaded again at
// the next connection (checked at most every CheckEvery), so that a renewed
// certificate needs no restart.
type Files struct {
	cert, key string

	mu      sync.Mutex
	current tls.Certificate
	stamp   string
	checked time.Time
	// OnReload is called after a changed certificate has been loaded, or with the error
	// if it cannot be loaded (the previous one is kept).
	OnReload func(tls.Certificate, error)
}

// CheckEvery is how often Files looks for changed files.
var CheckEvery = time.Minute

// LoadFiles loads the certificate and key from PEM files.
func LoadFiles(cert, key string) (*Files, error) {
	f := &Files{cert: cert, key: key}
	c, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return nil, explain(err, cert, key)
	}
	f.current, f.stamp, f.checked = c, stamp(cert, key), time.Now()
	return f, nil
}

func explain(err error, cert, key string) error {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("certificate or key not found (%s, %s): %w", cert, key, err)
	}
	return fmt.Errorf("cannot load the certificate %s with the key %s (PEM files expected): %w", cert, key, err)
}

func stamp(files ...string) string {
	var b strings.Builder
	for _, f := range files {
		if info, err := os.Stat(f); err == nil {
			fmt.Fprintf(&b, "%d %d;", info.Size(), info.ModTime().UnixNano())
		}
	}
	return b.String()
}

// Current returns the certificate, loading it again if its files have changed.
func (f *Files) Current() tls.Certificate {
	f.mu.Lock()
	defer f.mu.Unlock()
	if time.Since(f.checked) < CheckEvery {
		return f.current
	}
	f.checked = time.Now()
	s := stamp(f.cert, f.key)
	if s == f.stamp {
		return f.current
	}
	f.stamp = s
	c, err := tls.LoadX509KeyPair(f.cert, f.key)
	if err != nil {
		err = explain(err, f.cert, f.key)
	} else {
		f.current = c
	}
	if f.OnReload != nil {
		f.OnReload(c, err)
	}
	return f.current
}

// Config returns a TLS configuration that serves the current certificate.
func (f *Files) Config() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			c := f.Current()
			return &c, nil
		},
	}
}

// Static returns a TLS configuration for a fixed certificate.
func Static(c tls.Certificate) *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{c}}
}
