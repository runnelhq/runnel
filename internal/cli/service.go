package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/runnelhq/runnel/internal/logfile"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/winsvc"
)

// DefaultStopTimeout is the time running runs get to end when the service stops.
const DefaultStopTimeout = 30 * time.Second

func newServiceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Install and control Runnel as a Windows service",
		Long: "Runs the server of a project (like \"runnel serve\") as a Windows service, so that its\n" +
			"triggers run without anyone logged on. Install, uninstall, start and stop need a console\n" +
			"started as administrator. Several services (one per project) are told apart by --name.\n" +
			"The service writes its log to " + `%ProgramData%\runnel\logs\<name>.log` + "; start, stop, warnings\n" +
			"and errors also go to the Event Viewer (Windows Logs > Application, source <name>).",
		PersistentPreRunE: func(*cobra.Command, []string) error {
			if runtime.GOOS != "windows" {
				return winsvc.ErrNotSupported
			}
			return nil
		},
	}
	cmd.AddCommand(newServiceInstallCommand(), newServiceUninstallCommand(), newServiceStartCommand(),
		newServiceStopCommand(), newServiceStatusCommand(), newServiceRunCommand())
	return cmd
}

func nameFlag(cmd *cobra.Command, name *string) {
	cmd.Flags().StringVar(name, "name", winsvc.DefaultName, "name of the service")
}

// nameOpt is " --name x" for messages, empty for the default name.
func nameOpt(name string) string {
	if name == winsvc.DefaultName {
		return ""
	}
	return " --name " + name
}

// installOptions are the flags of "runnel service install".
type installOptions struct {
	name, account, start string
	port                 int
	stopTimeout          time.Duration
}

// serviceConfig checks the options and works out the service for the project in root,
// run by exe.
func serviceConfig(o installOptions, root, exe string) (winsvc.Config, error) {
	if err := winsvc.CheckName(o.name); err != nil {
		return winsvc.Config{}, err
	}
	if err := winsvc.CheckStart(o.start); err != nil {
		return winsvc.Config{}, err
	}
	if o.port < 1 || o.port > 65535 {
		return winsvc.Config{}, fmt.Errorf("invalid --port %d (expected 1–65535)", o.port)
	}
	if o.stopTimeout < 0 {
		return winsvc.Config{}, fmt.Errorf("invalid --stop-timeout %s (expected 0 or more, e.g. 30s)", o.stopTimeout)
	}
	display := "Runnel"
	if o.name != winsvc.DefaultName {
		display += " (" + o.name + ")"
	}
	return winsvc.Config{
		Name:        o.name,
		DisplayName: display,
		Description: fmt.Sprintf("Runnel server for the project %s (http://127.0.0.1:%d)", root, o.port),
		Exe:         exe,
		Args: []string{"service", "run", "--name", o.name, "--port", strconv.Itoa(o.port),
			"--stop-timeout", o.stopTimeout.String(), root},
		Account: winsvc.NormalizeAccount(o.account),
		Start:   o.start,
		LogDir:  winsvc.LogDir(),
	}, nil
}

func newServiceInstallCommand() *cobra.Command {
	var o installOptions
	cmd := &cobra.Command{
		Use:   "install [project-dir]",
		Short: "Install the service for a project",
		Long: "Installs a Windows service that serves the project in project-dir (or the current directory)\n" +
			"like \"runnel serve\" and starts its triggers. The service runs this runnel.exe; after moving\n" +
			"it, install the service again.\n\n" +
			"Account: LocalService by default (few rights, no password; it reaches network shares only if\n" +
			"they allow anyone to read). Use --account for another one: NetworkService, LocalSystem,\n" +
			"DOMAIN\\user (asks for the password; the account needs the right \"Log on as a service\") or a\n" +
			"group managed service account DOMAIN\\name$ (no password). The account must be able to read\n" +
			"the project folder.\n\n" +
			"Windows restarts the service after a failure (after 10s, 1m and 5m). When it stops, running\n" +
			"runs get --stop-timeout to end before they are cancelled.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			root, found, err := model.FindProjectDir(dir)
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("no project (%s) found in %s or above", model.ProjectFile, dir)
			}
			if root, err = filepath.Abs(root); err != nil {
				return err
			}
			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("cannot find runnel.exe: %w", err)
			}
			if exe, err = filepath.EvalSymlinks(exe); err != nil {
				return fmt.Errorf("cannot find runnel.exe: %w", err)
			}
			c, err := serviceConfig(o, root, exe)
			if err != nil {
				return err
			}
			if winsvc.NeedsPassword(c.Account) {
				if c.Password, err = winsvc.ReadPassword(cmd.OutOrStdout(), cmd.InOrStdin(), fmt.Sprintf("Password for %s: ", c.Account)); err != nil {
					return err
				}
			}
			if err := winsvc.Install(c); err != nil {
				return err
			}
			cmd.Printf("Installed service %s for the project %s\n", o.name, root)
			cmd.Printf("  account %s, start %s, http://127.0.0.1:%d, stop timeout %s\n", c.Account, o.start, o.port, o.stopTimeout)
			cmd.Printf("  log %s\n", filepath.Join(c.LogDir, o.name+".log"))
			cmd.Printf("Start it with: runnel service start%s\n", nameOpt(o.name))
			return nil
		},
	}
	nameFlag(cmd, &o.name)
	cmd.Flags().IntVar(&o.port, "port", DefaultPort, "TCP port on 127.0.0.1 for the API and the editor")
	cmd.Flags().StringVar(&o.account, "account", "LocalService", "account the service runs as")
	cmd.Flags().StringVar(&o.start, "start", winsvc.StartDelayed, "start type: auto, delayed (auto, shortly after Windows has started) or manual")
	cmd.Flags().DurationVar(&o.stopTimeout, "stop-timeout", DefaultStopTimeout, "time running runs get to end when the service stops, before they are cancelled")
	return cmd
}

func newServiceUninstallCommand() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Stop and remove the service",
		Long:  "Stops the service if it runs and removes it and its event source. The log files are kept.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := winsvc.CheckName(name); err != nil {
				return err
			}
			if err := winsvc.Uninstall(name, func(msg string) { cmd.Println(msg) }); err != nil {
				return err
			}
			cmd.Printf("Removed service %s; the log files in %s are kept\n", name, winsvc.LogDir())
			return nil
		},
	}
	nameFlag(cmd, &name)
	return cmd
}

func newServiceStartCommand() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := winsvc.CheckName(name); err != nil {
				return err
			}
			if err := winsvc.Start(name); err != nil {
				return err
			}
			cmd.Printf("Started service %s; see its state with: runnel service status%s\n", name, nameOpt(name))
			return nil
		},
	}
	nameFlag(cmd, &name)
	return cmd
}

func newServiceStopCommand() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the service",
		Long:  "Stops the service: no new runs are started, running runs get the stop timeout of the service to end.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := winsvc.CheckName(name); err != nil {
				return err
			}
			if err := winsvc.Stop(name, winsvc.StopWait, func(msg string) { cmd.Println(msg) }); err != nil {
				return err
			}
			cmd.Printf("Service %s stopped\n", name)
			return nil
		},
	}
	nameFlag(cmd, &name)
	return cmd
}

func newServiceStatusCommand() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the state of the service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := winsvc.CheckName(name); err != nil {
				return err
			}
			info, err := winsvc.Status(name)
			if err != nil {
				return err
			}
			cmd.Printf("Service:  %s (%s)\n", info.Name, info.DisplayName)
			cmd.Printf("State:    %s\n", info.State)
			cmd.Printf("Start:    %s\n", info.Start)
			cmd.Printf("Account:  %s\n", info.Account)
			cmd.Printf("Command:  %s\n", info.Command)
			cmd.Printf("Log:      %s\n", filepath.Join(winsvc.LogDir(), name+".log"))
			return nil
		},
	}
	nameFlag(cmd, &name)
	return cmd
}

// newServiceRunCommand is the command line of the installed service; Windows starts it.
func newServiceRunCommand() *cobra.Command {
	var name string
	var port int
	var stopTimeout time.Duration
	cmd := &cobra.Command{
		Use:    "run project-dir",
		Short:  "Run as the service (started by Windows)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if ok, err := winsvc.IsService(); err != nil || !ok {
				return fmt.Errorf(`"runnel service run" is started by Windows for an installed service; to run the server in this console, use "runnel serve"`)
			}
			return winsvc.Run(name, stopTimeout, func(ctx context.Context, events winsvc.Logger) error {
				return runService(ctx, events, name, args[0], port, stopTimeout)
			})
		},
	}
	nameFlag(cmd, &name)
	cmd.Flags().IntVar(&port, "port", DefaultPort, "TCP port on 127.0.0.1")
	cmd.Flags().DurationVar(&stopTimeout, "stop-timeout", DefaultStopTimeout, "time running runs get to end when stopping")
	return cmd
}

// runService serves the project for the service: the whole log goes to the log file,
// start, stop, warnings and errors also to the Event Viewer.
func runService(ctx context.Context, events winsvc.Logger, name, dir string, port int, stopTimeout time.Duration) error {
	path := filepath.Join(winsvc.LogDir(), name+".log")
	lf, err := logfile.Open(path, 0)
	if err != nil {
		events.Error(fmt.Sprintf("%s: %v", path, err))
		return err
	}
	defer func() { _ = lf.Close() }()
	line := func(level, msg string) {
		_, _ = fmt.Fprintf(lf, "%s %-5s %s\n", time.Now().Format("2006-01-02 15:04:05"), level, msg)
	}
	err = runServer(ctx, serveConfig{Dir: dir, Port: port, StopTimeout: stopTimeout,
		Log:   func(msg string) { line("INFO", msg) },
		Warn:  func(msg string) { line("WARN", msg); events.Warn(msg) },
		Print: func(msg string) { line("INFO", msg); events.Info(msg) },
	})
	if err != nil {
		line("ERROR", err.Error())
	}
	return err
}
