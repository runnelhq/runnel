package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/plugintrust"
	"github.com/runnelhq/runnel/internal/version"
)

// Session runs the actions of plugins for one run (action.PluginRunner): one process per
// plugin, started on first use after checking its approval again, ended by Close.
type Session struct {
	projectDir  string
	environment string
	store       plugintrust.Store
	mask        func(string) string

	mu     sync.Mutex
	procs  map[string]*Process
	failed map[string]error

	logMu   sync.Mutex
	current func(slog.Level, string) // the log of the action that called the plugin last
}

// NewSession prepares the plugins of a project for a run in an environment; mask (may be
// nil) hides the secrets of the run in everything that comes back from a plugin.
func NewSession(projectDir, environment string, store plugintrust.Store, mask func(string) string) *Session {
	return &Session{projectDir: projectDir, environment: environment, store: store, mask: mask,
		procs: map[string]*Process{}, failed: map[string]error{}}
}

// Execute runs an action of a plugin in its process for this run.
func (s *Session) Execute(ctx context.Context, plugin, typ string, config map[string]any, ec *action.ExecContext) (any, error) {
	pr, err := s.process(ctx, plugin, ec)
	if err != nil {
		return nil, err
	}
	return pr.Execute(ctx, typ, config, ExecContext{RunID: ec.RunID, ActionID: ec.ActionID, ProjectDir: s.projectDir, Environment: s.environment})
}

// process returns the process of a plugin for the run, starting it on first use. Lines
// on stderr go to the log of the action that called the plugin last.
func (s *Session) process(ctx context.Context, name string, ec *action.ExecContext) (*Process, error) {
	s.logMu.Lock()
	s.current = ec.Log
	s.logMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if pr := s.procs[name]; pr != nil {
		return pr, nil
	}
	if err := s.failed[name]; err != nil {
		return nil, err
	}
	err := s.start(ctx, name)
	if err != nil {
		s.failed[name] = err
	}
	return s.procs[name], err
}

func (s *Session) start(ctx context.Context, name string) error {
	plugins, _ := model.LoadPlugins(s.projectDir)
	p := plugins[name]
	if p == nil {
		return fmt.Errorf("plugin %q cannot be loaded; check its plugin.yaml with runnel validate", name)
	}
	if err := trusted(ctx, s.projectDir, p, s.store); err != nil {
		return err
	}
	pr, err := Start(ctx, p, Options{ProjectDir: s.projectDir, RunnelVersion: version.Version, Env: environ(s.projectDir),
		Mask: s.mask, Log: s.log})
	if err != nil {
		return err
	}
	s.procs[name] = pr
	return nil
}

func (s *Session) log(level slog.Level, msg string) {
	s.logMu.Lock()
	logf := s.current
	s.logMu.Unlock()
	if logf != nil {
		logf(level, msg)
	}
}

// Close ends the processes of the run.
func (s *Session) Close() {
	s.mu.Lock()
	procs := s.procs
	s.procs = map[string]*Process{}
	s.mu.Unlock()
	var wg sync.WaitGroup
	for _, pr := range procs {
		wg.Go(func() { _ = pr.Close() })
	}
	wg.Wait()
}

// environ is the environment of Runnel without the variables that hold the tokens of
// the http triggers of the project (ADR 0019): plugins get secrets only in their config.
func environ(projectDir string) []string {
	triggers, _ := model.LoadTriggers(projectDir)
	var drop []string
	for _, t := range triggers {
		if v := t.TokenVariable(); v != "" {
			drop = append(drop, v)
		}
	}
	env := os.Environ()
	if len(drop) == 0 {
		return env
	}
	return slices.DeleteFunc(env, func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return slices.ContainsFunc(drop, func(d string) bool { return strings.EqualFold(d, name) })
	})
}
