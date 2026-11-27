//go:build !windows

package winsvc

import (
	"io"
	"time"
)

// Install registers the service.
func Install(Config) error { return ErrNotSupported }

// Uninstall stops and removes the service.
func Uninstall(string, func(string)) error { return ErrNotSupported }

// Start starts the service.
func Start(string) error { return ErrNotSupported }

// Stop stops the service and waits up to wait until it has stopped.
func Stop(string, time.Duration, func(string)) error { return ErrNotSupported }

// Status describes the service.
func Status(string) (Info, error) { return Info{}, ErrNotSupported }

// IsService reports whether the program was started by the Service Control Manager.
func IsService() (bool, error) { return false, nil }

// Run runs the program as the service name until it is stopped.
func Run(string, time.Duration, RunFunc) error { return ErrNotSupported }

// ReadPassword reads a password from the console without showing it.
func ReadPassword(io.Writer, io.Reader, string) (string, error) { return "", ErrNotSupported }

// LogDir returns the folder of the log files of services.
func LogDir() string { return "" }

// HistoryDir returns the folder of the run histories of services.
func HistoryDir() string { return "" }

// TLSDir returns the folder of the self-signed certificate of services.
func TLSDir() string { return "" }

// TrustDir returns the folder of the plugin approvals of services.
func TrustDir() string { return "" }

// PrepareTrustDir creates the folder of the plugin approvals of services.
func PrepareTrustDir(string) error { return ErrNotSupported }

// SecretsDir returns the folder of the local secret store of services.
func SecretsDir() string { return "" }

// PrepareSecretsDir creates the folder of the local secret store of services.
func PrepareSecretsDir(string) error { return ErrNotSupported }
