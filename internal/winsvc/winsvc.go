// Package winsvc installs, controls and runs Runnel as a Windows service (roadmap S41):
// registration with the Service Control Manager, recovery after failures, the event
// source in the Event Viewer and the log folder. On other systems its functions return
// ErrNotSupported.
package winsvc

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ErrNotSupported is returned on systems other than Windows.
var ErrNotSupported = errors.New(`runnel service is only available on Windows; elsewhere run "runnel serve" with the service manager of the system (e.g. systemd)`)

// DefaultName is the name of the service unless --name is given.
const DefaultName = "runnel"

// Start types.
const (
	StartAuto    = "auto"
	StartDelayed = "delayed"
	StartManual  = "manual"
)

// Built-in accounts. LocalService is the default: few rights, no password.
const (
	AccountLocalService   = `NT AUTHORITY\LocalService`
	AccountNetworkService = `NT AUTHORITY\NetworkService`
	AccountLocalSystem    = "LocalSystem"
)

// Config describes a service to install.
type Config struct {
	Name        string
	DisplayName string
	Description string
	// Exe is the absolute path of runnel.exe; Args follow it on the command line.
	Exe  string
	Args []string
	// Account and Password: see NormalizeAccount and NeedsPassword.
	Account  string
	Password string
	// Start is auto, delayed (default) or manual.
	Start string
	// LogDir is created and made writable for the account.
	LogDir string
	// TLSDir is the folder of the self-signed certificate (--triggers-tls auto); it is
	// created for the account, Administrators and SYSTEM only. Empty: not needed.
	TLSDir string
}

// Info describes an installed service.
type Info struct {
	Name, DisplayName, State, Start, Account, Command string
}

// Logger writes to the Event Viewer (application log, source = service name).
type Logger interface {
	Info(msg string)
	Warn(msg string)
	Error(msg string)
}

// RunFunc runs the server of a service until ctx is done; events go to the Event Viewer.
type RunFunc func(ctx context.Context, events Logger) error

var namePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// CheckName checks a service name: a letter, then letters, digits, - and _.
func CheckName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid service name %q (a letter, then letters, digits, - or _; at most 64 characters)", name)
	}
	return nil
}

// CheckStart checks a start type.
func CheckStart(start string) error {
	switch start {
	case StartAuto, StartDelayed, StartManual:
		return nil
	}
	return fmt.Errorf("invalid start type %q (expected auto, delayed or manual)", start)
}

// NormalizeAccount turns the short names of the built-in accounts (LocalService,
// NetworkService, LocalSystem; case is ignored) into the names Windows expects;
// other accounts (DOMAIN\user, .\user, a gMSA ending in $) are kept.
func NormalizeAccount(account string) string {
	switch strings.ToLower(strings.TrimPrefix(strings.ToLower(account), `nt authority\`)) {
	case "", "localservice", "local service":
		return AccountLocalService
	case "networkservice", "network service":
		return AccountNetworkService
	case "localsystem", "local system", "system":
		return AccountLocalSystem
	}
	return account
}

// NeedsPassword reports whether an account needs a password: not the built-in
// accounts and not a group managed service account (gMSA, name ending in $).
func NeedsPassword(account string) bool {
	switch NormalizeAccount(account) {
	case AccountLocalService, AccountNetworkService, AccountLocalSystem:
		return false
	}
	return !strings.HasSuffix(account, "$")
}

// StopWait is how long "runnel service stop" and uninstall wait for the service to stop.
const StopWait = 2 * time.Minute
