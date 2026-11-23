// Package proc runs programs for the shell actions (ADR 0023): without a shell, with a
// timeout that ends the whole process tree, closed stdin unless given, stdout and stderr
// held up to a limit, and the output decoded as UTF-8 or the OEM code page.
package proc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// DefaultMaxOutput is the most bytes of stdout and of stderr kept (ADR 0023).
const DefaultMaxOutput = 1 << 20

// Encodings of the output.
const (
	EncodingAuto        = "auto"
	EncodingUTF8        = "utf-8"
	EncodingOEM         = "oem"
	EncodingWindows1252 = "windows-1252"
)

// Command is a program to run.
type Command struct {
	// Program is the path of the program (see Resolve).
	Program string
	Args    []string
	// Dir is the working folder; empty: the current one.
	Dir string
	// Env is the whole environment; nil: that of this process.
	Env []string
	// Input is written to stdin if HasInput; otherwise stdin is empty.
	Input    string
	HasInput bool
	// MaxOutput limits stdout and stderr each; 0: DefaultMaxOutput.
	MaxOutput int
	// Encoding of stdout and stderr; empty: auto.
	Encoding string
	// Timeout ends the program and all processes it started; 0: none.
	Timeout time.Duration
}

// Result is how the program ended.
type Result struct {
	ExitCode  int
	Stdout    string
	Stderr    string
	Truncated bool
	Duration  time.Duration
}

// ErrTimeout is returned when the program ran longer than its timeout.
var ErrTimeout = errors.New("timeout")

// batchSpecial are the characters cmd.exe interprets in arguments of batch files.
const batchSpecial = "&|<>^%!\"\r\n"

// CheckBatchArgs rejects arguments with characters that cmd.exe would interpret, for
// .bat and .cmd files, which always run through cmd.exe (ADR 0023): strictly, also where
// they could be escaped.
func CheckBatchArgs(program string, args []string) error {
	switch strings.ToLower(filepath.Ext(program)) {
	case ".bat", ".cmd":
	default:
		return nil
	}
	for i, a := range args {
		if j := strings.IndexAny(a, batchSpecial); j >= 0 {
			return fmt.Errorf("args[%d]: %q contains %q, which cmd.exe interprets in arguments of batch files; pass the value in env or use a PowerShell script", i, a, string(a[j]))
		}
	}
	return nil
}

// Resolve finds the program: a name without folder through PATH, a path relative to
// base (the project folder, ADR 0020).
func Resolve(program, base string) (string, error) {
	if program == "" {
		return "", errors.New("program is empty")
	}
	if !strings.ContainsAny(program, `/\`) {
		p, err := exec.LookPath(program)
		if err != nil {
			return "", fmt.Errorf("program %q not found in PATH", program)
		}
		return p, nil
	}
	if !filepath.IsAbs(program) && !strings.HasPrefix(program, `\\`) && base != "" {
		program = filepath.Join(base, program)
	}
	p, err := exec.LookPath(program)
	if err != nil {
		return "", fmt.Errorf("program %s not found or not executable", program)
	}
	return p, nil
}

// Run runs a program and waits for it. A program that cannot be started is an error; one
// that ends with any exit code is not. On timeout or cancellation the whole process tree
// is ended and ErrTimeout or the error of ctx returned with the result so far.
func Run(ctx context.Context, c Command) (Result, error) {
	if err := CheckBatchArgs(c.Program, c.Args); err != nil {
		return Result{}, err
	}
	most := c.MaxOutput
	if most <= 0 {
		most = DefaultMaxOutput
	}
	stdout, stderr := &limited{max: most}, &limited{max: most}
	// Not CommandContext: cancelling ends the whole process tree below, not only the program.
	cmd := exec.Command(c.Program, c.Args...) //nolint:gosec,noctx // starting a program is the purpose (ADR 0023); no shell, arguments one by one
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = c.Dir, c.Env, stdout, stderr
	if c.HasInput {
		cmd.Stdin = strings.NewReader(c.Input)
	}
	// Processes that keep stdout open after the program ended do not hold up the end.
	cmd.WaitDelay = 2 * time.Second
	prepare(cmd)
	start := time.Now()
	tree, err := startTree(cmd)
	if err != nil {
		return Result{}, fmt.Errorf("cannot start %s: %w", filepath.Base(c.Program), err)
	}
	defer tree.release()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var timeout <-chan time.Time
	if c.Timeout > 0 {
		t := time.NewTimer(c.Timeout)
		defer t.Stop()
		timeout = t.C
	}
	var stopped error
	select {
	case err = <-done:
	case <-timeout:
		stopped = ErrTimeout
	case <-ctx.Done():
		stopped = ctx.Err()
	}
	if stopped != nil {
		tree.kill()
		err = <-done
	}
	res := Result{ExitCode: -1, Duration: time.Since(start), Truncated: stdout.truncated || stderr.truncated,
		Stdout: decode(stdout.buf.Bytes(), c.Encoding), Stderr: decode(stderr.buf.Bytes(), c.Encoding)}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	if stopped != nil {
		return res, stopped
	}
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) && !errors.Is(err, exec.ErrWaitDelay) {
		return res, err
	}
	return res, nil
}

// limited keeps the first max bytes written to it and drops the rest, so that the
// program is never blocked by a full pipe.
type limited struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (l *limited) Write(p []byte) (int, error) {
	if room := l.max - l.buf.Len(); room < len(p) {
		l.truncated = true
		if room > 0 {
			l.buf.Write(p[:room])
		}
		return len(p), nil
	}
	l.buf.Write(p)
	return len(p), nil
}

// decode reads the output in an encoding: auto is UTF-8 if valid, else the OEM code
// page of Windows (Windows-1252 elsewhere).
func decode(b []byte, encoding string) string {
	switch encoding {
	case EncodingUTF8:
		return strings.ToValidUTF8(string(b), "�")
	case EncodingWindows1252:
		return decodeWith(b, charmap.Windows1252)
	case EncodingOEM:
		return decodeWith(b, oemCharmap())
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return decodeWith(b, oemCharmap())
}

func decodeWith(b []byte, cm *charmap.Charmap) string {
	s, err := cm.NewDecoder().Bytes(b)
	if err != nil {
		return strings.ToValidUTF8(string(b), "�")
	}
	return string(s)
}

// codePages are the OEM code pages of Windows that are decoded; others count as 850.
var codePages = map[uint32]*charmap.Charmap{
	437: charmap.CodePage437, 850: charmap.CodePage850, 852: charmap.CodePage852,
	855: charmap.CodePage855, 858: charmap.CodePage858, 860: charmap.CodePage860,
	862: charmap.CodePage862, 863: charmap.CodePage863, 865: charmap.CodePage865,
	866: charmap.CodePage866, 1252: charmap.Windows1252,
}

func oemCharmap() *charmap.Charmap {
	if cm := codePages[oemCodePage()]; cm != nil {
		return cm
	}
	if oemCodePage() == 0 {
		return charmap.Windows1252 // not Windows
	}
	return charmap.CodePage850
}

// tree ends a started program with all processes it started.
type tree struct {
	kill    func()
	release func()
}

// Valid reports whether an encoding is known.
func Valid(encoding string) bool {
	switch encoding {
	case "", EncodingAuto, EncodingUTF8, EncodingOEM, EncodingWindows1252:
		return true
	}
	return false
}
