// Package plugin runs plugins (ADR 0019): one process per plugin and run, spoken to with
// JSON-RPC over stdin/stdout (package jsonrpc), its stderr as log. Whether a plugin may
// start at all (its approval) is checked by the caller with package plugintrust.
package plugin

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/jsonrpc"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/proc"
)

// ProtocolVersion is the version of the protocol this host speaks (ADR 0019, Nachtrag 2026-11-24).
const ProtocolVersion = 1

// Time limits of the protocol (ADR 0019).
const (
	StartTimeout    = 10 * time.Second // initialize and describe
	ShutdownTimeout = 5 * time.Second  // after shutdown, then the process is ended
	maxLogLine      = 64 << 10
)

// Options of a plugin process.
type Options struct {
	// ProjectDir is the working folder of the plugin (ADR 0019, relative paths as in Runnel).
	ProjectDir string
	// RunnelVersion is sent with initialize.
	RunnelVersion string
	// Env is the environment of the plugin (nil: that of Runnel); RUNNEL_PLUGIN=1 is added.
	Env []string
	// Log receives the lines of stderr (with "<plugin>: " before them) and nil drops them.
	Log func(level slog.Level, msg string)
	// Mask replaces secrets in everything that comes back from the plugin: log lines,
	// error messages and outputs (nil: nothing is replaced).
	Mask func(string) string
}

// Info is the answer of the plugin to initialize.
type Info struct {
	ProtocolVersion int    `json:"protocolVersion"`
	Encoding        string `json:"encoding"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	// Concurrency is how many requests the plugin handles at once; 0 or missing: 1.
	Concurrency int `json:"concurrency"`
}

// Process is a running plugin.
type Process struct {
	plugin *model.Plugin
	opts   Options
	cmd    *exec.Cmd
	tree   *proc.Tree
	stdin  io.WriteCloser
	conn   *jsonrpc.Conn
	slots  chan struct{}
	exited chan struct{}
	logs   sync.WaitGroup
	// Info is the answer to initialize.
	Info Info

	mu     sync.Mutex
	stderr []string // the last lines, for messages when the plugin ends unexpectedly
}

// Program returns the program and arguments of a plugin for this computer: executable
// relative to the folder of the plugin or as a command through PATH, and arguments that
// name a file in the folder as its full path.
func Program(p *model.Plugin) (string, []string, error) {
	exe, ok := p.ExecutableFor("")
	if !ok {
		return "", nil, fmt.Errorf("plugin %s has no executable for %s", p.Name, runtime.GOOS)
	}
	dir, err := filepath.Abs(p.Dir)
	if err != nil {
		return "", nil, err
	}
	local := filepath.Join(dir, filepath.FromSlash(exe))
	if !filepath.IsAbs(exe) && (strings.ContainsAny(exe, `/\`) || isFile(local)) {
		exe = local
	}
	program, err := proc.Resolve(exe, "")
	if err != nil {
		return "", nil, fmt.Errorf("plugin %s: %w", p.Name, err)
	}
	args := make([]string, len(p.Args))
	for i, a := range p.Args {
		args[i] = a
		if f := filepath.Join(dir, filepath.FromSlash(a)); !filepath.IsAbs(a) && isFile(f) {
			args[i] = f
		}
	}
	return program, args, nil
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// Start starts a plugin and does the handshake (initialize). The process runs until Close.
func Start(ctx context.Context, p *model.Plugin, o Options) (*Process, error) {
	program, args, err := Program(p)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(program, args...) //nolint:gosec,noctx // starting the approved plugin is the purpose (ADR 0019); ended through its process tree
	cmd.Dir = o.ProjectDir
	env := o.Env
	if env == nil {
		env = os.Environ()
	}
	cmd.Env = append(append([]string(nil), env...), "RUNNEL_PLUGIN=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	tree, err := proc.StartTree(cmd)
	if err != nil {
		return nil, fmt.Errorf("cannot start plugin %s: %w", p.Name, err)
	}
	pr := &Process{plugin: p, opts: o, cmd: cmd, tree: tree, stdin: stdin, exited: make(chan struct{})}
	pr.logs.Add(1)
	go pr.readLog(stderr)
	pr.conn = jsonrpc.New(stdout, stdin, nil)
	go func() {
		// stdout and stderr are read to the end before Wait closes the pipes.
		pr.logs.Wait()
		<-pr.conn.Done()
		_ = cmd.Wait()
		close(pr.exited)
	}()

	ictx, cancel := context.WithTimeout(ctx, StartTimeout)
	defer cancel()
	err = pr.call(ictx, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion, "encodings": []string{"json"},
		"runnel": map[string]string{"version": o.RunnelVersion},
	}, &pr.Info)
	switch {
	case err != nil:
	case pr.Info.ProtocolVersion != ProtocolVersion:
		err = fmt.Errorf("protocol version %d is not supported (Runnel speaks %d)", pr.Info.ProtocolVersion, ProtocolVersion)
	case pr.Info.Encoding != "" && pr.Info.Encoding != "json":
		err = fmt.Errorf("encoding %q is not supported (expected json)", pr.Info.Encoding)
	case pr.Info.Name != p.Name:
		err = fmt.Errorf("the program answers as plugin %q, not %q", pr.Info.Name, p.Name)
	}
	if err != nil {
		pr.kill()
		return nil, fmt.Errorf("plugin %s: initialize: %w", p.Name, err)
	}
	pr.slots = make(chan struct{}, max(pr.Info.Concurrency, 1))
	return pr, nil
}

// call is a request with the errors of a plugin that ended: its last lines of stderr.
func (pr *Process) call(ctx context.Context, method string, params, result any) error {
	err := pr.conn.Call(ctx, method, params, result)
	if err == nil {
		return nil
	}
	if rpcErr, ok := errors.AsType[*jsonrpc.Error](err); ok {
		return errors.New(pr.mask(rpcErr.Message))
	}
	select {
	case <-pr.conn.Done():
		<-pr.exited
		msg := "the plugin ended unexpectedly"
		if e := pr.conn.Err(); e != nil && !errors.Is(e, jsonrpc.ErrClosed) {
			msg = "the plugin sent something invalid: " + e.Error()
		}
		if last := pr.lastLines(); last != "" {
			msg += "; last lines: " + last
		}
		return errors.New(pr.mask(msg))
	default:
		return err
	}
}

// Describe returns the action types of the plugin (describe), checked: types in the
// namespace of the plugin, kind action, a schema of the configuration. Transforms follow
// with S50 and are ignored until then.
func (pr *Process) Describe(ctx context.Context) ([]action.Descriptor, error) {
	ctx, cancel := context.WithTimeout(ctx, StartTimeout)
	defer cancel()
	var res struct {
		Actions []action.Descriptor `json:"actions"`
	}
	if err := pr.call(ctx, "describe", struct{}{}, &res); err != nil {
		return nil, fmt.Errorf("plugin %s: describe: %w", pr.plugin.Name, err)
	}
	seen := map[string]bool{}
	for i := range res.Actions {
		d := &res.Actions[i]
		switch {
		case !model.ValidTypeName(d.Type):
			return nil, fmt.Errorf("plugin %s: invalid type name %q", pr.plugin.Name, d.Type)
		case model.TypeNamespace(d.Type) != pr.plugin.Namespace:
			return nil, fmt.Errorf("plugin %s: type %s is outside its namespace %q", pr.plugin.Name, d.Type, pr.plugin.Namespace)
		case seen[d.Type]:
			return nil, fmt.Errorf("plugin %s: type %s is described twice", pr.plugin.Name, d.Type)
		case d.Kind != "" && d.Kind != "action":
			return nil, fmt.Errorf("plugin %s: %s has kind %q in actions", pr.plugin.Name, d.Type, d.Kind)
		}
		seen[d.Type] = true
		d.Kind = "action"
		if d.Schema == nil {
			return nil, fmt.Errorf("plugin %s: %s has no schema", pr.plugin.Name, d.Type)
		}
		if d.Label == "" {
			d.Label = d.Type
		}
	}
	return res.Actions, nil
}

// ExecContext is what an action of a plugin learns about its run.
type ExecContext struct {
	RunID       string `json:"runId"`
	ActionID    string `json:"actionId"`
	ProjectDir  string `json:"projectDir"`
	Environment string `json:"environment,omitempty"`
}

// Execute runs an action of the plugin (action.execute) with its resolved configuration
// and returns its output. Without concurrency in initialize, requests wait for each
// other; cancelling ctx sends $/cancelRequest.
func (pr *Process) Execute(ctx context.Context, typ string, config map[string]any, ec ExecContext) (any, error) {
	select {
	case pr.slots <- struct{}{}:
		defer func() { <-pr.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	var res struct {
		Output any `json:"output"`
	}
	params := map[string]any{"type": typ, "config": config, "context": ec}
	if err := pr.call(ctx, "action.execute", params, &res); err != nil {
		return nil, err
	}
	return pr.maskValue(res.Output), nil
}

// Close ends the plugin: shutdown, then stdin closed; a plugin that does not end within
// ShutdownTimeout is ended with its process tree.
func (pr *Process) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()
	_ = pr.conn.Call(ctx, "shutdown", struct{}{}, nil)
	_ = pr.stdin.Close()
	select {
	case <-pr.exited:
	case <-ctx.Done():
		pr.tree.Kill()
		<-pr.exited
	}
	pr.tree.Release()
	return nil
}

func (pr *Process) kill() {
	_ = pr.stdin.Close()
	pr.tree.Kill()
	<-pr.exited
	pr.tree.Release()
}

// readLog passes the lines of stderr to the log: a prefix DEBUG, INFO, WARN or ERROR
// sets the level (otherwise info), at most maxLogLine bytes each.
func (pr *Process) readLog(r io.Reader) {
	defer pr.logs.Done()
	br := bufio.NewReaderSize(r, maxLogLine)
	for {
		line, err := br.ReadSlice('\n')
		if len(line) > 0 {
			text := pr.mask(strings.TrimRight(string(line), "\r\n"))
			if err == bufio.ErrBufferFull { //nolint:errorlint // ReadSlice returns it as is
				text += " …"
				for err == bufio.ErrBufferFull { //nolint:errorlint // skip the rest of the line
					_, err = br.ReadSlice('\n')
				}
			}
			pr.log(text)
		}
		if err != nil {
			return
		}
	}
}

func (pr *Process) log(text string) {
	pr.mu.Lock()
	pr.stderr = append(pr.stderr, text)
	if len(pr.stderr) > 5 {
		pr.stderr = pr.stderr[1:]
	}
	pr.mu.Unlock()
	if pr.opts.Log == nil || strings.TrimSpace(text) == "" {
		return
	}
	level := slog.LevelInfo
	for _, l := range []struct {
		prefix string
		level  slog.Level
	}{{"DEBUG", slog.LevelDebug}, {"INFO", slog.LevelInfo}, {"WARN", slog.LevelWarn}, {"ERROR", slog.LevelError}} {
		if rest, ok := strings.CutPrefix(text, l.prefix); ok && (rest == "" || rest[0] == ' ' || rest[0] == ':') {
			level, text = l.level, strings.TrimLeft(rest, ": ")
			break
		}
	}
	pr.opts.Log(level, pr.plugin.Name+": "+text)
}

func (pr *Process) lastLines() string {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	return strings.Join(pr.stderr, " | ")
}

func (pr *Process) mask(s string) string {
	if pr.opts.Mask == nil {
		return s
	}
	return pr.opts.Mask(s)
}

// maskValue masks the texts in an output of the plugin (nested lists and maps too).
func (pr *Process) maskValue(v any) any {
	if pr.opts.Mask == nil {
		return v
	}
	switch t := v.(type) {
	case string:
		return pr.opts.Mask(t)
	case []any:
		for i := range t {
			t[i] = pr.maskValue(t[i])
		}
	case map[string]any:
		for k := range t {
			t[k] = pr.maskValue(t[k])
		}
	}
	return v
}
