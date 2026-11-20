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
	cmd := &cobra.Command{
		Use:   "serve [project-dir]",
		Short: "Start the server for a project (API for the editor)",
		Long: "Starts the server for the project in project-dir (or the current directory), found by its\n" +
			"project.yaml in that directory or above. The server listens on 127.0.0.1 only and serves the\n" +
			"API under " + server.APIPrefix + " (files, catalog, validation, runs, live events).\n" +
			"Stop it with Ctrl+C; running workflows are cancelled.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			if port < 0 || port > 65535 {
				return fmt.Errorf("invalid --port %d (expected 0–65535; 0 picks a free port)", port)
			}
			return serve(cmd, dir, port)
		},
	}
	cmd.Flags().IntVar(&port, "port", DefaultPort, "TCP port on 127.0.0.1 (0 picks a free port)")
	return cmd
}

func serve(cmd *cobra.Command, dir string, port int) error {
	root, found, err := model.FindProjectDir(dir)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no project (%s) found in %s or above", model.ProjectFile, dir)
	}
	srv, err := server.New(server.Options{Dir: root, Registry: actions.Builtin(), Transforms: transforms.Builtin()})
	if err != nil {
		return err
	}
	defer func() { _ = srv.Close() }()

	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("cannot listen on port %d (is another runnel serve running? use --port): %w", port, err)
	}
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}

	ctx := cmd.Context()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// First cancel the runs and end the event streams, then stop the HTTP server.
		if err := srv.Shutdown(shutdown); err != nil {
			cmd.PrintErrln("warning: not all runs ended in time")
		}
		_ = hs.Shutdown(shutdown)
	}()

	cmd.Printf("Serving project %s\n", root)
	cmd.Printf("Listening on http://%s (API under %s); press Ctrl+C to stop\n", ln.Addr(), server.APIPrefix)
	err = hs.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		<-stopped
		cmd.Println("Server stopped")
		return nil
	}
	return err
}
