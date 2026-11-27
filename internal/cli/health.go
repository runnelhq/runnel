package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/runnelhq/runnel/internal/httpx"
	"github.com/runnelhq/runnel/internal/server"
	"github.com/runnelhq/runnel/internal/winsvc"
)

// Exit codes of "runnel health", as monitoring plugins (Nagios, Checkmk, PRTG) read them.
const (
	healthExitOK          = 0
	healthExitDegraded    = 1
	healthExitFail        = 2
	healthExitUnreachable = 3
)

func newHealthCommand() *cobra.Command {
	var addr, serviceName string
	var asJSON bool
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Show the health of a running server or service",
		Long: "Asks the running \"runnel serve\" or service on this computer for its health (" + server.APIPrefix + "/health):\n" +
			"scheduler, trigger files, failed runs, run history, disk space and the listener of http triggers.\n" +
			"--service-name reads the port from the installed Windows service, else --addr is used.\n\n" +
			"Exit code: 0 ok, 1 degraded, 2 fail, 3 not reachable or unknown (as monitoring tools expect).",
		Args: cobra.NoArgs,
		// An invalid command line leaves the health unknown.
		Annotations: map[string]string{annotationUsageExit: strconv.Itoa(healthExitUnreachable)},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if serviceName != "" {
				port, err := servicePort(serviceName)
				if err != nil {
					return healthUnknown(cmd, err)
				}
				addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
			}
			h, err := fetchHealth(cmd.Context(), addr, timeout)
			if err != nil {
				return healthUnknown(cmd, err)
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(h); err != nil {
					return err
				}
			} else {
				printHealth(cmd.OutOrStdout(), h)
			}
			switch h.Status {
			case server.HealthOK:
				return nil
			case server.HealthDegraded:
				return errExitCode(healthExitDegraded)
			}
			return errExitCode(healthExitFail)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", net.JoinHostPort("127.0.0.1", strconv.Itoa(DefaultPort)), "address of the server on this computer")
	cmd.Flags().StringVar(&serviceName, "service-name", "", "name of an installed Windows service (e.g. runnel): use its port")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the answer of the server as JSON")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Second, "how long to wait for the answer")
	return cmd
}

// healthUnknown reports an error with exit code 3: the health is unknown.
func healthUnknown(cmd *cobra.Command, err error) error {
	cmd.PrintErrln("Error:", err)
	return errExitCode(healthExitUnreachable)
}

// fetchHealth asks the server at addr (on this computer) for its health.
func fetchHealth(ctx context.Context, addr string, timeout time.Duration) (*server.Health, error) {
	if !httpx.IsLoopback(addr) {
		return nil, fmt.Errorf("invalid --addr %q: the server listens on this computer only (e.g. 127.0.0.1:%d)", addr, DefaultPort)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	url := "http://" + addr + server.APIPrefix + "/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid --addr %q: %w", addr, err)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}} // never through a proxy
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("no runnel server reachable at %s (is it running? see --addr, --service-name): %w", addr, err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("cannot read the answer of %s: %w", url, err)
	}
	var h server.Health
	if err := json.Unmarshal(body, &h); err != nil || h.Status == "" {
		return nil, fmt.Errorf("%s answered %s without a health report (an older runnel or another program?)", url, res.Status)
	}
	return &h, nil
}

var portArg = regexp.MustCompile(`--port[= ]"?(\d+)`)

// servicePort returns the port of an installed service from its command line.
func servicePort(name string) (int, error) {
	if err := winsvc.CheckName(name); err != nil {
		return 0, err
	}
	info, err := winsvc.Status(name)
	if err != nil {
		return 0, err
	}
	return portOf(name, info.Command)
}

// portOf returns the port in the command line of a service: --port, else the default.
func portOf(name, command string) (int, error) {
	m := portArg.FindStringSubmatch(command)
	if m == nil {
		return DefaultPort, nil
	}
	port, err := strconv.Atoi(m[1])
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("service %s: invalid port in its command line %q", name, command)
	}
	return port, nil
}

// printHealth writes a readable summary of a health report.
func printHealth(w io.Writer, h *server.Health) {
	p := func(format string, args ...any) { _, _ = fmt.Fprintf(w, format, args...) }
	p("Status:   %s\n", h.Status)
	p("Server:   runnel %s, up %s (since %s)\n", h.Version, (time.Duration(h.UptimeSeconds) * time.Second).String(), h.StartedAt.Format(time.RFC3339))
	p("Project:  %s (%s)\n", h.Project.Name, h.Project.Dir)
	p("Checks:\n")
	for _, c := range h.Checks {
		p("  %-9s %-12s %s\n", c.Status, c.Name, c.Message)
	}
	for _, pr := range h.Scheduler.Problems {
		pos := pr.File
		if pr.Line > 0 {
			pos += fmt.Sprintf(":%d:%d", pr.Line, pr.Column)
		}
		p("  %s: %s: %s\n", pos, pr.Severity, pr.Message)
	}
	if f := h.Runs.LastFailed; f != nil {
		what := f.Path
		if f.Trigger != "" {
			what += " (" + f.Trigger + ")"
		}
		p("Last failed run: %s %s at %s\n", f.ID, what, f.Time.Format(time.RFC3339))
		if f.Error != "" {
			p("  %s\n", firstLine(f.Error))
		}
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
