package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/proc"
)

// execute runs a prepared command for an action of the namespace (ADR 0023): env is added
// to the environment and its values are hidden in the output; the output has exitCode,
// stdout, stderr, truncated, duration and, with readJSON, json.
func execute(ctx context.Context, ec *action.ExecContext, name string, cmd proc.Command, env map[string]string, okCodes []int, readJSON bool) (action.Result, error) {
	var secrets []string
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
			secrets = append(secrets, v)
		}
	}
	res, err := proc.Run(ctx, cmd)
	// Values of env (e.g. secrets) never leave in the output.
	res.Stdout, res.Stderr = hide(res.Stdout, secrets), hide(res.Stderr, secrets)
	switch {
	case errors.Is(err, proc.ErrTimeout):
		return action.Result{}, fmt.Errorf("%s took longer than %s and was ended with the processes it started", name, cmd.Timeout)
	case err != nil:
		return action.Result{}, err
	}
	if res.Truncated {
		ec.Log(slog.LevelWarn, fmt.Sprintf("the output of %s was longer than %d bytes; the rest was dropped", name, maxOutput(cmd.MaxOutput)))
	}
	out := map[string]any{"exitCode": int64(res.ExitCode), "stdout": res.Stdout, "stderr": res.Stderr,
		"truncated": res.Truncated, "duration": res.Duration.Round(time.Millisecond).String()}
	if !slices.Contains(okCodes, res.ExitCode) {
		return action.Result{Output: out}, fmt.Errorf("%s ended with exit code %d%s", name, res.ExitCode, lastLines(res.Stderr, 5))
	}
	if readJSON {
		v, err := lastJSON(res.Stdout)
		if err != nil {
			return action.Result{Output: out}, fmt.Errorf("the output of %s is not JSON: %w", name, err)
		}
		out["json"] = v
	}
	ec.Log(slog.LevelInfo, fmt.Sprintf("%s ended with exit code %d in %s", name, res.ExitCode, res.Duration.Round(time.Millisecond)))
	return action.Result{Output: out}, nil
}

// checkEnvName reports a name that cannot be an environment variable.
func checkEnvName(name string) error {
	if name == "" || strings.ContainsAny(name, "=\x00") {
		return fmt.Errorf("env: invalid name %q", name)
	}
	return nil
}

// lastJSON reads the output as JSON: all of it, or else the last JSON value, which starts
// at a line beginning with { or [ (e.g. ConvertTo-Json after other output).
func lastJSON(text string) (any, error) {
	var v any
	err := json.Unmarshal([]byte(text), &v)
	if err == nil {
		return v, nil
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range slices.Backward(lines) {
		if l := strings.TrimSpace(line); strings.HasPrefix(l, "{") || strings.HasPrefix(l, "[") {
			if json.Unmarshal([]byte(strings.Join(lines[i:], "\n")), &v) == nil {
				return v, nil
			}
		}
	}
	return nil, err
}

// outputLimit reads maxOutput, a size like 1MB (ADR 0025); empty is the default.
func outputLimit(text string) (int, error) {
	n, err := format.ParseSize("maxOutput", text, proc.DefaultMaxOutput)
	if err != nil {
		return 0, err
	}
	if n > 1<<30 {
		return 0, fmt.Errorf("maxOutput: %q is larger than 1GB", text)
	}
	return int(n), nil
}

func maxOutput(n int) int {
	if n <= 0 {
		return proc.DefaultMaxOutput
	}
	return n
}

// hide replaces the given values in a text.
func hide(text string, values []string) string {
	for _, v := range values {
		if len(v) >= 3 {
			text = strings.ReplaceAll(text, v, "***")
		}
	}
	return text
}

// lastLines returns the last n lines of stderr for a message, ": …" or empty.
func lastLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(text, "\r\n", "\n"), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	s := strings.TrimSpace(strings.Join(lines, "\n"))
	if s == "" {
		return ""
	}
	return ": " + s
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
