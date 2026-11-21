package cli

import (
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/runnelhq/runnel/internal/tlscert"
	"github.com/runnelhq/runnel/internal/triggers"
)

// listenerOptions configure the listener of http triggers (S38a, S40a).
type listenerOptions struct {
	// Address, e.g. 127.0.0.1:7301 or 0.0.0.0:7301; empty: no listener.
	Address string
	// TLS is "" or auto (a self-signed certificate).
	TLS string
	// Cert and Key are the PEM files of an own certificate.
	Cert, Key string
	// Insecure allows plain HTTP on the network.
	Insecure bool
}

func listenerFlags(cmd *cobra.Command, o *listenerOptions) {
	f := cmd.Flags()
	f.StringVar(&o.Address, "triggers", triggers.DefaultListen, "address of the listener of http triggers, e.g. 0.0.0.0:7301 for the network (\"\": none)")
	f.StringVar(&o.TLS, "triggers-tls", "", "auto: HTTPS with a self-signed certificate, created once and reused")
	f.StringVar(&o.Cert, "triggers-cert", "", "HTTPS with an own certificate: PEM file of the certificate (with --triggers-key)")
	f.StringVar(&o.Key, "triggers-key", "", "PEM file of the private key of --triggers-cert")
	f.BoolVar(&o.Insecure, "triggers-insecure", false, "allow plain HTTP on the network (tokens and data are sent as plain text)")
}

// secure reports whether the listener serves HTTPS.
func (o listenerOptions) secure() bool {
	return o.TLS != "" || o.Cert != ""
}

// args returns the options as flags for the command line of a service.
func (o listenerOptions) args() []string {
	args := []string{"--triggers", o.Address}
	if o.TLS != "" {
		args = append(args, "--triggers-tls", o.TLS)
	}
	if o.Cert != "" {
		args = append(args, "--triggers-cert", o.Cert, "--triggers-key", o.Key)
	}
	if o.Insecure {
		args = append(args, "--triggers-insecure")
	}
	return args
}

// check checks the options: on the network only with TLS, unless --triggers-insecure.
func (o listenerOptions) check() error {
	switch {
	case o.TLS != "" && o.TLS != "auto":
		return fmt.Errorf("invalid --triggers-tls %q (expected auto, or --triggers-cert and --triggers-key)", o.TLS)
	case (o.Cert == "") != (o.Key == ""):
		return fmt.Errorf("--triggers-cert and --triggers-key belong together")
	case o.TLS != "" && o.Cert != "":
		return fmt.Errorf("use either --triggers-tls auto or --triggers-cert and --triggers-key")
	case o.Insecure && o.secure():
		return fmt.Errorf("--triggers-insecure is for plain HTTP; leave it out with TLS")
	}
	if o.Address == "" {
		return nil
	}
	host, port, err := net.SplitHostPort(o.Address)
	if err != nil {
		return fmt.Errorf("invalid --triggers %q (expected an address with a port, e.g. %s or 0.0.0.0:7301)", o.Address, triggers.DefaultListen)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("invalid --triggers %q (port 0–65535)", o.Address)
	}
	local := host == "localhost"
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		local = true
	}
	if !local && !o.secure() && !o.Insecure {
		return fmt.Errorf("--triggers %s listens on the network: use HTTPS with --triggers-tls auto or --triggers-cert and --triggers-key; "+
			"for plain HTTP in a trusted network add --triggers-insecure", o.Address)
	}
	return nil
}

// absolute makes the paths of an own certificate absolute (for a service).
func (o listenerOptions) absolute() (listenerOptions, error) {
	for _, p := range []*string{&o.Cert, &o.Key} {
		if *p == "" {
			continue
		}
		abs, err := filepath.Abs(*p)
		if err != nil {
			return o, err
		}
		*p = abs
	}
	return o, nil
}

// tlsConfig returns the TLS configuration of the listener (nil for plain HTTP) and logs
// the certificate. dir is the folder of the self-signed certificate.
func (o listenerOptions) tlsConfig(dir string, log, warn func(string)) (*tls.Config, error) {
	switch {
	case o.Address == "" || !o.secure():
		return nil, nil
	case o.Cert != "":
		f, err := tlscert.LoadFiles(o.Cert, o.Key)
		if err != nil {
			return nil, err
		}
		f.OnReload = func(c tls.Certificate, err error) {
			if err != nil {
				warn("http triggers: the changed certificate cannot be loaded, the previous one stays: " + err.Error())
				return
			}
			log("http triggers: loaded the renewed certificate " + tlscert.Describe(c))
		}
		log("http triggers: certificate " + o.Cert + " " + tlscert.Describe(f.Current()))
		return f.Config(), nil
	}
	dns, ips := tlscert.Names()
	c, created, err := tlscert.SelfSigned(dir, dns, ips, time.Now())
	if err != nil {
		return nil, fmt.Errorf("self-signed certificate in %s: %w", dir, err)
	}
	what := "using the self-signed certificate"
	if created {
		what = "created a self-signed certificate"
	}
	log(fmt.Sprintf("http triggers: %s %s; callers can trust it by importing %s",
		what, tlscert.Describe(c), filepath.Join(dir, tlscert.CertFile)))
	return tlscert.Static(c), nil
}

// userTLSDir is the folder of the self-signed certificate of "runnel serve":
// %LOCALAPPDATA%\runnel\tls on Windows, the configuration folder of the user elsewhere.
func userTLSDir() string {
	base, err := os.UserConfigDir()
	if runtime.GOOS == "windows" {
		base, err = os.UserCacheDir() // %LOCALAPPDATA%
	}
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "runnel", "tls")
}
