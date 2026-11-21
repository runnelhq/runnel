package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/server"
	"github.com/runnelhq/runnel/internal/transforms"
)

// DefaultPort is the port of "runnel serve" unless --port is given.
const DefaultPort = 7300

func newServeCommand() *cobra.Command {
	var port int
	var stopTimeout time.Duration
	var listen listenerOptions
	cmd := &cobra.Command{
		Use:   "serve [project-dir]",
		Short: "Start the server for a project (API for the editor)",
		Long: "Starts the server for the project in project-dir (or the current directory), found by its\n" +
			"project.yaml in that directory or above. The server listens on 127.0.0.1 only and serves the\n" +
			"API under " + server.APIPrefix + " (files, catalog, validation, runs, live events).\n" +
			"It starts the runs of the project's triggers (" + model.TriggersDir + "/*.yaml) at their times; changed\n" +
			"trigger files are picked up while it runs. HTTP triggers get their own listener (--triggers),\n" +
			"opened once an http trigger is enabled. On the network it needs HTTPS: --triggers-tls auto creates\n" +
			"a certificate once (in " + userTLSDir() + "), --triggers-cert and --triggers-key use an own one.\n" +
			"Stop it with Ctrl+C; running workflows are cancelled, after --stop-timeout if given.\n" +
			"To run it as a Windows service, see \"runnel service install\".",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			logf := func(msg string) { cmd.Printf("%s %s\n", time.Now().Format("15:04:05"), msg) }
			return runServer(cmd.Context(), serveConfig{Dir: dir, Port: port, StopTimeout: stopTimeout, Triggers: listen, TLSDir: userTLSDir(),
				Log: logf, Warn: logf, Print: func(msg string) { cmd.Println(msg) }, StopHint: "; press Ctrl+C to stop"})
		},
	}
	cmd.Flags().IntVar(&port, "port", DefaultPort, "TCP port on 127.0.0.1 (0 picks a free port)")
	listenerFlags(cmd, &listen)
	cmd.Flags().DurationVar(&stopTimeout, "stop-timeout", 0, "time running runs get to end when stopping, before they are cancelled (e.g. 30s; 0: cancel at once)")
	return cmd
}

// serveConfig configures runServer, shared by "runnel serve" and the Windows service.
type serveConfig struct {
	Dir  string
	Port int
	// StopTimeout is the time running runs get to end when ctx is done, before they
	// are cancelled; no new runs are started in that time.
	StopTimeout time.Duration
	// Log and Warn receive the messages of the server log; Print the lines about the
	// server itself (project, address, stopped).
	Log, Warn, Print func(msg string)
	// StopHint follows the line with the address, e.g. "; press Ctrl+C to stop".
	StopHint string
	// Triggers configures the listener of http triggers; TLSDir is the folder of its
	// self-signed certificate.
	Triggers listenerOptions
	TLSDir   string
}

// runServer serves the project until ctx is done.
func runServer(ctx context.Context, c serveConfig) error {
	if c.Port < 0 || c.Port > 65535 {
		return fmt.Errorf("invalid --port %d (expected 0–65535; 0 picks a free port)", c.Port)
	}
	if err := c.Triggers.check(); err != nil {
		return err
	}
	if c.StopTimeout < 0 {
		return fmt.Errorf("invalid --stop-timeout %s (expected 0 or more, e.g. 30s)", c.StopTimeout)
	}
	root, found, err := model.FindProjectDir(c.Dir)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no project (%s) found in %s or above", model.ProjectFile, c.Dir)
	}
	tlsConfig, err := c.Triggers.tlsConfig(c.TLSDir, c.Log, c.Warn)
	if err != nil {
		return err
	}
	srv, err := server.New(server.Options{Dir: root, Registry: actions.Builtin(), Transforms: transforms.Builtin(), Log: c.Log, Warn: c.Warn,
		TriggerListen: c.Triggers.Address, TriggerTLS: tlsConfig})
	if err != nil {
		return err
	}
	defer func() { _ = srv.Close() }()

	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(c.Port)))
	if err != nil {
		return fmt.Errorf("cannot listen on port %d (is another runnel serve or service running? use --port): %w", c.Port, err)
	}
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		if n := srv.Running(); c.StopTimeout > 0 && n > 0 {
			c.Log(fmt.Sprintf("stopping: no new runs; waiting up to %s for %s to end", c.StopTimeout, plural(n, "running run")))
			drain, cancel := context.WithTimeout(context.Background(), c.StopTimeout)
			err := srv.Drain(drain)
			cancel()
			if err != nil {
				c.Warn(fmt.Sprintf("stopping: cancelling %s still running after %s", plural(srv.Running(), "run"), c.StopTimeout))
			}
		}
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// First cancel the runs and end the event streams, then stop the HTTP server.
		if err := srv.Shutdown(shutdown); err != nil {
			c.Warn("warning: not all runs ended in time")
		}
		_ = hs.Shutdown(shutdown)
	}()

	c.Print(fmt.Sprintf("Serving project %s", root))
	c.Print(fmt.Sprintf("Listening on http://%s (API under %s)%s", ln.Addr(), server.APIPrefix, c.StopHint))
	srv.StartTriggers(ctx)
	err = hs.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		<-stopped
		c.Print("Server stopped")
		return nil
	}
	return err
}
