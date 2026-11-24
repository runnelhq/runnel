package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/plugin"
	"github.com/runnelhq/runnel/internal/plugintrust"
	"github.com/runnelhq/runnel/internal/version"
	"github.com/runnelhq/runnel/internal/winsvc"
)

type pluginsOptions struct {
	names   []string
	yes     bool
	service bool
}

func newPluginsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugins [project-dir]",
		Short: "List the plugins of a project and whether they are trusted on this computer",
		Long: "Lists the plugins of the project in project-dir (or the current directory): the folders\n" +
			"plugins/<name>/ with a plugin.yaml (ADR 0019). A plugin is a program that runs with the\n" +
			"rights of Runnel; it only starts once it is trusted on this computer for this project\n" +
			"(\"runnel plugins trust\"). Any change in its folder voids the approval. Trusted plugins\n" +
			"are started to list the types they offer.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return listPlugins(cmd.Context(), cmd.OutOrStdout(), dirArg(args))
		},
	}
	var o pluginsOptions
	trust := &cobra.Command{
		Use:   "trust [project-dir]",
		Short: "Trust the plugins of a project on this computer",
		Long: "Shows each plugin of the project that is not trusted yet (name, version, namespace, program\n" +
			"and the hash of its folder) and asks whether to trust it. Approvals are kept outside the\n" +
			"project: for the user in the configuration folder (%AppData%\\runnel on Windows), with\n" +
			"--service for the Windows service in %ProgramData%\\runnel\\trust (as administrator).",
		Example: "  runnel plugins trust\n" +
			"  runnel plugins trust D:\\flows --plugin iban --yes\n" +
			"  runnel plugins trust D:\\flows --service",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return trustPlugins(cmd.OutOrStdout(), cmd.InOrStdin(), dirArg(args), o, time.Now())
		},
	}
	untrust := &cobra.Command{
		Use:   "untrust [project-dir]",
		Short: "Withdraw the approval of plugins of a project",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return untrustPlugins(cmd.OutOrStdout(), dirArg(args), o)
		},
	}
	for _, c := range []*cobra.Command{trust, untrust} {
		c.Flags().StringArrayVar(&o.names, "plugin", nil, "only this plugin (repeatable); default: all plugins of the project")
		c.Flags().BoolVar(&o.service, "service", false, "for the Windows service instead of the user (needs administrator rights)")
	}
	trust.Flags().BoolVar(&o.yes, "yes", false, "trust without asking (for scripts)")
	cmd.AddCommand(trust, untrust)
	return cmd
}

func dirArg(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	return "."
}

// projectPlugins loads the project of dir and its plugins; problems in plugin files are
// printed (they keep the plugin from loading), names selects plugins by name.
func projectPlugins(out io.Writer, dir string, names []string) (string, []*model.Plugin, error) {
	root, found, err := model.FindProjectDir(dir)
	if err != nil {
		return "", nil, err
	}
	if !found {
		return "", nil, fmt.Errorf("no project (%s) found in %s or above", model.ProjectFile, dir)
	}
	plugins, err := model.LoadPlugins(displayPath(root))
	if diags, ok := errors.AsType[model.Errors](err); ok {
		for _, d := range diags {
			_, _ = fmt.Fprintln(out, formatDiagnostic(d))
		}
	} else if err != nil {
		return "", nil, err
	}
	var list []*model.Plugin
	for _, name := range slices.Sorted(maps.Keys(plugins)) {
		if len(names) == 0 || slices.Contains(names, name) {
			list = append(list, plugins[name])
		}
	}
	for _, n := range names {
		if plugins[n] == nil {
			return "", nil, fmt.Errorf("plugin %q not found (or not loaded) in %s", n, filepath.Join(displayPath(root), model.PluginsDir))
		}
	}
	return root, list, nil
}

func trustStore() (plugintrust.Store, error) {
	return plugintrust.DefaultStore(winsvc.TrustDir())
}

func listPlugins(ctx context.Context, out io.Writer, dir string) error {
	root, plugins, err := projectPlugins(out, dir, nil)
	if err != nil {
		return err
	}
	if len(plugins) == 0 {
		_, _ = fmt.Fprintf(out, "No plugins in %s.\n", filepath.Join(displayPath(root), model.PluginsDir))
		return nil
	}
	store, err := trustStore()
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tVERSION\tNAMESPACE\tPROGRAM\tSTATUS")
	var trusted []*model.Plugin
	for _, p := range plugins {
		program, ok := p.ExecutableFor("")
		if !ok {
			program = "– (none for " + runtime.GOOS + ")"
		}
		status := statusText(store, root, p)
		if strings.HasPrefix(status, string(plugintrust.Trusted)) {
			trusted = append(trusted, p)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", p.Name, p.Version, p.Namespace, program, status)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	// Trusted plugins are started to show their types (describe, S49b1).
	for _, p := range trusted {
		_, _ = fmt.Fprintf(out, "\nTypes of %s:\n", p.Name)
		types, err := describePlugin(ctx, root, p)
		if err != nil {
			_, _ = fmt.Fprintf(out, "  error: %v\n", err)
			continue
		}
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		for _, d := range types.Actions {
			_, _ = fmt.Fprintf(tw, "  %s\taction\t%s\n", d.Type, d.Description)
		}
		for _, d := range types.Transforms {
			_, _ = fmt.Fprintf(tw, "  %s\ttransform\t%s\n", d.Type, d.Description)
		}
		for _, c := range types.Connections {
			_, _ = fmt.Fprintf(tw, "  %s\tconnection\t%s\n", c.Type, c.Description)
		}
		if len(types.Actions)+len(types.Transforms)+len(types.Connections) == 0 {
			_, _ = fmt.Fprintln(tw, "  (none)")
		}
		_ = tw.Flush()
	}
	return nil
}

// describePlugin starts a trusted plugin, asks for its types and ends it again.
func describePlugin(ctx context.Context, root string, p *model.Plugin) (plugin.Description, error) {
	pr, err := plugin.Start(ctx, p, plugin.Options{ProjectDir: root, RunnelVersion: version.Version})
	if err != nil {
		return plugin.Description{}, err
	}
	defer func() { _ = pr.Close() }()
	return pr.Describe(ctx)
}

// statusText describes the approval of a plugin for the list.
func statusText(store plugintrust.Store, root string, p *model.Plugin) string {
	st, err := store.Check(root, p.Name, p.Dir)
	switch {
	case err != nil:
		return "error: " + err.Error()
	case st.State == plugintrust.Trusted && st.Service:
		return "trusted (service)"
	case st.State == plugintrust.Changed:
		return "changed since trusted"
	}
	return string(st.State)
}

func trustPlugins(out io.Writer, in io.Reader, dir string, o pluginsOptions, now time.Time) error {
	root, plugins, err := projectPlugins(out, dir, o.names)
	if err != nil {
		return err
	}
	store, err := trustStore()
	if err != nil {
		return err
	}
	if o.service && store.Service == "" {
		return errors.New("--service: approvals for the service exist only on Windows")
	}
	if o.service {
		if err := winsvc.PrepareTrustDir(filepath.Dir(store.Service)); err != nil {
			return fmt.Errorf("%w (run the console as administrator)", err)
		}
	}
	answers := bufio.NewReader(in)
	var asked int
	for _, p := range plugins {
		st, err := store.Check(root, p.Name, p.Dir)
		if err != nil {
			return fmt.Errorf("plugin %s: %w", p.Name, err)
		}
		if st.State == plugintrust.Trusted && st.Service == o.service {
			_, _ = fmt.Fprintf(out, "Plugin %s is already trusted.\n", p.Name)
			continue
		}
		program, _ := p.ExecutableFor("")
		_, _ = fmt.Fprintf(out, "Plugin %s %s (namespace %s)\n  folder:  %s\n  program: %s %s\n  hash:    %s\n",
			p.Name, p.Version, p.Namespace, displayPath(p.Dir), program, strings.Join(p.Args, " "), st.Hash)
		if st.State == plugintrust.Changed {
			_, _ = fmt.Fprintln(out, "  Files of the plugin changed since it was trusted.")
		}
		if !o.yes {
			asked++
			_, _ = fmt.Fprintf(out, "A plugin runs with the rights of Runnel%s. Trust it only if you know where it comes from.\nTrust plugin %s? [y/N] ",
				map[bool]string{true: " (of the account of the service)", false: ""}[o.service], p.Name)
			line, _ := answers.ReadString('\n')
			if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
				_, _ = fmt.Fprintf(out, "Plugin %s is not trusted.\n", p.Name)
				continue
			}
		}
		if err := store.Trust(root, p.Name, p.Version, st.Hash, o.service, now); err != nil {
			return fmt.Errorf("plugin %s: %w", p.Name, err)
		}
		_, _ = fmt.Fprintf(out, "Plugin %s is trusted%s.\n", p.Name, map[bool]string{true: " for the service", false: ""}[o.service])
	}
	if len(plugins) == 0 {
		_, _ = fmt.Fprintf(out, "No plugins in %s.\n", filepath.Join(displayPath(root), model.PluginsDir))
	}
	return nil
}

func untrustPlugins(out io.Writer, dir string, o pluginsOptions) error {
	root, plugins, err := projectPlugins(out, dir, o.names)
	if err != nil {
		return err
	}
	store, err := trustStore()
	if err != nil {
		return err
	}
	for _, p := range plugins {
		removed, err := store.Untrust(root, p.Name, o.service)
		if err != nil {
			return fmt.Errorf("plugin %s: %w", p.Name, err)
		}
		if removed {
			_, _ = fmt.Fprintf(out, "Plugin %s is no longer trusted.\n", p.Name)
		} else {
			_, _ = fmt.Fprintf(out, "Plugin %s was not trusted.\n", p.Name)
		}
	}
	return nil
}
