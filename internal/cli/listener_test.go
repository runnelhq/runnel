package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/tlscert"
)

func TestListenerOptions(t *testing.T) {
	for _, o := range []listenerOptions{
		{},
		{Address: "127.0.0.1:7301"},
		{Address: "[::1]:0"},
		{Address: "localhost:8080", TLS: "auto"},
		{Address: "0.0.0.0:7301", TLS: "auto"},
		{Address: "192.0.2.10:7301", Cert: "a.crt", Key: "a.key"},
		{Address: ":7301", Insecure: true},
	} {
		if err := o.check(); err != nil {
			t.Errorf("%+v: %v", o, err)
		}
	}
	for _, c := range []struct {
		o    listenerOptions
		want string
	}{
		{listenerOptions{Address: "7301"}, "expected an address with a port"},
		{listenerOptions{Address: "127.0.0.1:x"}, "port 0–65535"},
		{listenerOptions{Address: "0.0.0.0:7301"}, "listens on the network: use HTTPS with --triggers-tls auto or --triggers-cert and --triggers-key; for plain HTTP in a trusted network add --triggers-insecure"},
		{listenerOptions{Address: ":7301"}, "listens on the network"},
		{listenerOptions{Address: ":7301", TLS: "on"}, `invalid --triggers-tls "on"`},
		{listenerOptions{Address: ":7301", Cert: "a.crt"}, "--triggers-cert and --triggers-key belong together"},
		{listenerOptions{Address: ":7301", TLS: "auto", Cert: "a.crt", Key: "a.key"}, "use either --triggers-tls auto or"},
		{listenerOptions{Address: ":7301", TLS: "auto", Insecure: true}, "--triggers-insecure is for plain HTTP"},
	} {
		if err := c.o.check(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v, want %q", c.o, err, c.want)
		}
	}
	o := listenerOptions{Address: ":7301", Cert: "a.crt", Key: "a.key", Insecure: false}
	abs, err := o.absolute()
	if err != nil || !filepath.IsAbs(abs.Cert) || !slices.Equal(abs.args()[:3], []string{"--triggers", ":7301", "--triggers-cert"}) {
		t.Errorf("absolute %+v %v", abs, err)
	}
	if got := (listenerOptions{Address: ":1", Insecure: true}).args(); !slices.Equal(got, []string{"--triggers", ":1", "--triggers-insecure"}) {
		t.Errorf("args %v", got)
	}
}

// --triggers-tls auto creates the certificate once and reuses it; an own certificate
// is loaded from its files.
func TestListenerTLS(t *testing.T) {
	dir := t.TempDir()
	var logs []string
	logf := func(msg string) { logs = append(logs, msg) }
	o := listenerOptions{Address: "127.0.0.1:0", TLS: "auto"}
	for range 2 {
		if cfg, err := o.tlsConfig(dir, logf, logf); err != nil || cfg == nil || len(cfg.Certificates) != 1 {
			t.Fatalf("auto: %v", err)
		}
	}
	if len(logs) != 2 || !strings.Contains(logs[0], "created a self-signed certificate") || !strings.Contains(logs[1], "using the self-signed certificate") ||
		!strings.Contains(logs[1], "callers can trust it by importing "+filepath.Join(dir, tlscert.CertFile)) {
		t.Errorf("logs %v", logs)
	}
	own := listenerOptions{Address: "127.0.0.1:0", Cert: filepath.Join(dir, tlscert.CertFile), Key: filepath.Join(dir, tlscert.KeyFile)}
	if cfg, err := own.tlsConfig("", logf, logf); err != nil || cfg.GetCertificate == nil || !strings.Contains(logs[2], "http triggers: certificate ") {
		t.Errorf("own: %v %v", err, logs)
	}
	if err := os.Remove(own.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := own.tlsConfig("", logf, logf); err == nil || !strings.Contains(err.Error(), "certificate or key not found") {
		t.Errorf("missing key: %v", err)
	}
	if cfg, err := (listenerOptions{Address: "127.0.0.1:0"}).tlsConfig(dir, logf, logf); cfg != nil || err != nil {
		t.Errorf("plain: %v %v", cfg, err)
	}
}
