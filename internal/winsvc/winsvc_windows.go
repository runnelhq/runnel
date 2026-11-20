//go:build windows

package winsvc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

// Event ids in the Event Viewer.
const (
	eventInfo  = 1
	eventWarn  = 2
	eventError = 3
)

// Recovery after a failure: restart after 10 seconds, then after 1 and 5 minutes; the
// count starts again after a day without failures.
var recovery = []mgr.RecoveryAction{
	{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
	{Type: mgr.ServiceRestart, Delay: time.Minute},
	{Type: mgr.ServiceRestart, Delay: 5 * time.Minute},
}

const recoveryReset = 24 * 60 * 60 // seconds

// connect connects to the Service Control Manager.
func connect() (*mgr.Mgr, error) {
	m, err := mgr.Connect()
	if err != nil {
		return nil, explain(err, "")
	}
	return m, nil
}

// explain turns errors of the Service Control Manager into messages for users.
func explain(err error, name string) error {
	switch {
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return errors.New("access denied; run the command in a console started as administrator")
	case errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST):
		return fmt.Errorf("service %q is not installed (install it with \"runnel service install\")", name)
	case errors.Is(err, windows.ERROR_SERVICE_EXISTS):
		return fmt.Errorf("service %q is already installed; remove it first with \"runnel service uninstall --name %s\" or choose another --name", name, name)
	case errors.Is(err, windows.ERROR_SERVICE_LOGON_FAILED):
		return fmt.Errorf("service %q cannot log on with its account: check the password, and that the account has the right \"Log on as a service\" (local security policy)", name)
	}
	return err
}

func open(m *mgr.Mgr, name string) (*mgr.Service, error) {
	s, err := m.OpenService(name)
	if err != nil {
		return nil, explain(err, name)
	}
	return s, nil
}

// Install registers the service, its recovery actions and its event source and
// prepares the log folder.
func Install(c Config) error {
	m, err := connect()
	if err != nil {
		return err
	}
	defer func() { _ = m.Disconnect() }()
	account := NormalizeAccount(c.Account)
	cfg := mgr.Config{
		DisplayName:      c.DisplayName,
		Description:      c.Description,
		StartType:        mgr.StartAutomatic,
		DelayedAutoStart: c.Start == StartDelayed || c.Start == "",
		ServiceStartName: account,
		Password:         c.Password,
	}
	if c.Start == StartManual {
		cfg.StartType = mgr.StartManual
	}
	if account == AccountLocalSystem {
		cfg.ServiceStartName = "" // LocalSystem
	}
	s, err := m.CreateService(c.Name, c.Exe, cfg, c.Args...)
	if err != nil {
		return explain(err, c.Name)
	}
	defer func() { _ = s.Close() }()
	undo := func(cause error) error {
		_ = s.Delete()
		return cause
	}
	if err := s.SetRecoveryActions(recovery, recoveryReset); err != nil {
		return undo(fmt.Errorf("cannot set the recovery actions: %w", err))
	}
	// Also restart when the server ends with an error (e.g. its port is taken), not
	// only when it crashes.
	if err := s.SetRecoveryActionsOnNonCrashFailures(true); err != nil {
		return undo(fmt.Errorf("cannot set the recovery actions: %w", err))
	}
	if err := eventlog.InstallAsEventCreate(c.Name, eventlog.Error|eventlog.Warning|eventlog.Info); err != nil &&
		!strings.Contains(err.Error(), "exists") {
		return undo(fmt.Errorf("cannot register the event source %q: %w", c.Name, err))
	}
	if err := prepareLogDir(c.LogDir, account); err != nil {
		_ = eventlog.Remove(c.Name)
		return undo(err)
	}
	return nil
}

// prepareLogDir creates the log folder and lets the account write to it (modify).
func prepareLogDir(dir, account string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create the log folder %s: %w", dir, err)
	}
	if account == AccountLocalSystem {
		return nil
	}
	sid, _, _, err := windows.LookupSID("", account)
	if err != nil {
		return fmt.Errorf("account %q not found: %w", account, err)
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("cannot read the permissions of %s: %w", dir, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("cannot read the permissions of %s: %w", dir, err)
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_READ | windows.GENERIC_WRITE | windows.GENERIC_EXECUTE | windows.DELETE,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}}, dacl)
	if err != nil {
		return fmt.Errorf("cannot set the permissions of %s: %w", dir, err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		return fmt.Errorf("cannot set the permissions of %s: %w", dir, err)
	}
	return nil
}

// Uninstall stops the service if it runs, removes it and its event source. The log
// files are kept.
func Uninstall(name string, progress func(string)) error {
	if err := Stop(name, StopWait, progress); err != nil && !errors.Is(err, errNotRunning) {
		return err
	}
	m, err := connect()
	if err != nil {
		return err
	}
	defer func() { _ = m.Disconnect() }()
	s, err := open(m, name)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	if err := s.Delete(); err != nil {
		return explain(err, name)
	}
	_ = eventlog.Remove(name)
	return nil
}

// Start starts the service.
func Start(name string) error {
	m, err := connect()
	if err != nil {
		return err
	}
	defer func() { _ = m.Disconnect() }()
	s, err := open(m, name)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	if err := s.Start(); err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
			return fmt.Errorf("service %q is already running", name)
		}
		return explain(err, name)
	}
	return nil
}

var errNotRunning = errors.New("not running")

// Stop stops the service and waits up to wait until it has stopped; progress gets a
// message while it waits.
func Stop(name string, wait time.Duration, progress func(string)) error {
	m, err := connect()
	if err != nil {
		return err
	}
	defer func() { _ = m.Disconnect() }()
	s, err := open(m, name)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	st, err := s.Query()
	if err != nil {
		return explain(err, name)
	}
	if st.State == svc.Stopped {
		return fmt.Errorf("service %q is not running: %w", name, errNotRunning)
	}
	if st.State != svc.StopPending {
		if _, err := s.Control(svc.Stop); err != nil {
			return explain(err, name)
		}
	}
	progress(fmt.Sprintf("Stopping service %s (running runs get the time of --stop-timeout to end) ...", name))
	for deadline := time.Now().Add(wait); ; time.Sleep(300 * time.Millisecond) {
		st, err := s.Query()
		if err != nil {
			return explain(err, name)
		}
		if st.State == svc.Stopped {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("service %q has not stopped after %s; see the log file and the Event Viewer", name, wait)
		}
	}
}

// Status describes the service.
func Status(name string) (Info, error) {
	m, err := connect()
	if err != nil {
		return Info{}, err
	}
	defer func() { _ = m.Disconnect() }()
	s, err := open(m, name)
	if err != nil {
		return Info{}, err
	}
	defer func() { _ = s.Close() }()
	st, err := s.Query()
	if err != nil {
		return Info{}, explain(err, name)
	}
	c, err := s.Config()
	if err != nil {
		return Info{}, explain(err, name)
	}
	info := Info{Name: name, DisplayName: c.DisplayName, State: stateName(st.State), Account: c.ServiceStartName, Command: c.BinaryPathName}
	switch {
	case c.StartType == mgr.StartManual:
		info.Start = StartManual
	case c.StartType == mgr.StartDisabled:
		info.Start = "disabled"
	case c.DelayedAutoStart:
		info.Start = StartDelayed
	default:
		info.Start = StartAuto
	}
	if info.Account == "" {
		info.Account = AccountLocalSystem
	}
	return info, nil
}

func stateName(s svc.State) string {
	switch s {
	case svc.Stopped:
		return "stopped"
	case svc.StartPending:
		return "starting"
	case svc.StopPending:
		return "stopping"
	case svc.Running:
		return "running"
	case svc.ContinuePending, svc.PausePending, svc.Paused:
		return "paused"
	}
	return fmt.Sprintf("unknown (%d)", s)
}

// IsService reports whether the program was started by the Service Control Manager.
func IsService() (bool, error) {
	return svc.IsWindowsService()
}

// LogDir returns the folder of the log files of services: %ProgramData%\runnel\logs.
func LogDir() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, "runnel", "logs")
}

// events writes to the Event Viewer; if the source cannot be opened, nothing is written.
type events struct{ l *eventlog.Log }

func (e events) Info(msg string) {
	if e.l != nil {
		_ = e.l.Info(eventInfo, msg)
	}
}

func (e events) Warn(msg string) {
	if e.l != nil {
		_ = e.l.Warning(eventWarn, msg)
	}
}

func (e events) Error(msg string) {
	if e.l != nil {
		_ = e.l.Error(eventError, msg)
	}
}

// Run runs the program as the service name until it is stopped: it calls run with a
// context that is cancelled when Windows stops the service. stopTimeout is the time
// the server needs to stop at most (announced to Windows). If run ends with an error,
// the service stops with an error, so that its recovery actions restart it.
func Run(name string, stopTimeout time.Duration, run RunFunc) error {
	l, _ := eventlog.Open(name)
	ev := events{l}
	defer func() {
		if l != nil {
			_ = l.Close()
		}
	}()
	return svc.Run(name, &handler{run: run, events: ev, stopHint: stopTimeout + 15*time.Second})
}

type handler struct {
	run      RunFunc
	events   events
	stopHint time.Duration
}

func (h *handler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.run(ctx, h.events) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done:
			if err != nil {
				h.events.Error("the server has stopped: " + err.Error())
				return true, 1
			}
			return false, 0
		case r := <-requests:
			switch r.Cmd {
			case svc.Interrogate:
				status <- r.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, WaitHint: uint32(h.stopHint / time.Millisecond)}
				cancel()
				if err := <-done; err != nil {
					h.events.Error("the server has stopped: " + err.Error())
				}
				return false, 0
			}
		}
	}
}

// ReadPassword reads a password from the console without showing it; if in is not a
// console (e.g. a pipe in a script), a line is read from it.
func ReadPassword(out io.Writer, in io.Reader, prompt string) (string, error) {
	_, _ = fmt.Fprint(out, prompt)
	if f, ok := in.(*os.File); ok {
		h := windows.Handle(f.Fd())
		var mode uint32
		if windows.GetConsoleMode(h, &mode) == nil {
			if err := windows.SetConsoleMode(h, mode&^windows.ENABLE_ECHO_INPUT); err == nil {
				defer func() {
					_ = windows.SetConsoleMode(h, mode)
					_, _ = fmt.Fprintln(out)
				}()
			}
		}
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		return "", fmt.Errorf("cannot read the password: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
