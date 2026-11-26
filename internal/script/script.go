// Package script runs JavaScript for core.script (ADR 0028) in a goja runtime without
// access to the host: no file system, network, processes or environment. A runtime
// offers console.log/warn/error (to a callback, with limits) and require for modules
// from the folder scripts/ of the project; context cancellation interrupts a script.
//
// A Runtime is not safe for concurrent use: one per execution of the action, or one per
// copy of the transform, which calls its code with a Session (S73b).
package script

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/dop251/goja"
)

// Limits of a runtime (ADR 0028).
const (
	// MaxLines is the number of console lines per execution; then one hint follows.
	MaxLines = 1000
	// MaxLineBytes is the longest console line; longer ones are cut.
	MaxLineBytes = 8 << 10
	// MaxResultBytes is the largest result of a function, measured as JSON.
	MaxResultBytes = 16 << 20
	// maxCallStack keeps runaway recursion from exhausting the Go stack.
	maxCallStack = 10000
)

// Options configure a runtime.
type Options struct {
	// Dir is the project folder: require loads modules from Dir/scripts. Empty: require
	// is not available.
	Dir string
	// Log receives the lines of console.log (info), console.warn and console.error; nil
	// drops them.
	Log func(level slog.Level, msg string)
}

// Runtime is a JavaScript runtime with console and require.
type Runtime struct {
	vm      *goja.Runtime
	opts    Options
	modules map[string]*goja.Object
	lines   int
}

// New creates a runtime.
func New(opts Options) *Runtime {
	r := &Runtime{vm: goja.New(), opts: opts, modules: map[string]*goja.Object{}}
	r.vm.SetMaxCallStackSize(maxCallStack)
	console := r.vm.NewObject()
	for name, level := range map[string]slog.Level{"log": slog.LevelInfo, "info": slog.LevelInfo, "warn": slog.LevelWarn, "error": slog.LevelError} {
		_ = console.Set(name, r.consoleFunc(level))
	}
	_ = r.vm.Set("console", console)
	_ = r.vm.Set("require", r.require)
	return r
}

// Function is compiled JavaScript code, called with Call.
type Function struct {
	name string
	fn   goja.Callable
}

// Compile makes a function from the body of a function (code with return) with the
// given parameter names; name stands for the code in messages, e.g. "script" or a file.
// The lines of the body keep their numbers in messages.
func (r *Runtime) Compile(name, body string, params ...string) (*Function, error) {
	prg, err := parse(name, body, params)
	if err != nil {
		return nil, err
	}
	v, err := r.vm.RunProgram(prg)
	if err != nil {
		return nil, r.failure(name, err)
	}
	fn, ok := goja.AssertFunction(v)
	if !ok {
		return nil, fmt.Errorf("%s: not a function", name)
	}
	return &Function{name: name, fn: fn}, nil
}

// Check reports syntax errors of code as Compile (fn: the body of a function with the
// given parameters) or Session.Run (not fn) would, without a runtime.
func Check(name, code string, fn bool, params ...string) error {
	if fn {
		_, err := parse(name, code, params)
		return err
	}
	if _, err := goja.Compile(name, code, false); err != nil {
		return syntaxError(name, err)
	}
	return nil
}

// parse compiles the body of a function.
func parse(name, body string, params []string) (*goja.Program, error) {
	src := "(function(" + strings.Join(params, ", ") + ") {" + body + "\n})"
	prg, err := goja.Compile(name, src, false)
	if err != nil {
		return nil, syntaxError(name, err)
	}
	return prg, nil
}

// Call runs a function with Go values as arguments (see Value) and returns its result
// as Go value (see Export). Cancelling ctx interrupts the script; the error is then
// ctx.Err(). The count of console lines starts anew with each call.
func (r *Runtime) Call(ctx context.Context, f *Function, args ...any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.lines = 0
	values := make([]goja.Value, len(args))
	for i, a := range args {
		values[i] = r.Value(a)
	}
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		r.vm.Interrupt(ctx.Err())
		close(interrupted)
	})
	v, err := f.fn(goja.Undefined(), values...)
	if !stop() {
		<-interrupted // an interrupt must not reach the next call
	}
	r.vm.ClearInterrupt()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, r.failure(f.name, err)
	}
	return r.Export(v)
}

// failure turns an error of goja into a message for users: the message of the thrown
// value with the line of the script it was thrown in.
func (r *Runtime) failure(name string, err error) error {
	if _, ok := errors.AsType[*goja.StackOverflowError](err); ok {
		return fmt.Errorf("%s: too much recursion (more than %d nested calls)", name, maxCallStack)
	}
	ex, ok := errors.AsType[*goja.Exception](err)
	if !ok {
		return err
	}
	msg := ex.Value().String()
	if obj, ok := ex.Value().(*goja.Object); ok {
		if m := obj.Get("message"); m != nil && !goja.IsUndefined(m) && isError(obj) {
			msg = m.String()
			if n := obj.Get("name"); n != nil && n.String() != "Error" {
				msg = n.String() + ": " + msg
			}
		}
	}
	for _, f := range ex.Stack() {
		if f.SrcName() != "" && f.SrcName() != "<native>" {
			p := f.Position()
			return fmt.Errorf("%s, line %d: %s", f.SrcName(), p.Line, msg)
		}
	}
	return fmt.Errorf("%s: %s", name, msg)
}

// isError reports an instance of Error or of one of its subclasses.
func isError(obj *goja.Object) bool {
	return obj.ClassName() == "Error"
}

// syntaxError shortens a compile error of goja to "name, line n: message".
func syntaxError(name string, err error) error {
	if se, ok := errors.AsType[*goja.CompilerSyntaxError](err); ok && se.File != nil {
		p := se.File.Position(se.Offset)
		return fmt.Errorf("%s, line %d: syntax error: %s", name, p.Line, se.Message)
	}
	return fmt.Errorf("%s: %w", name, err)
}

// consoleFunc writes the arguments of a console call as one line: texts as they are,
// other values as JSON where possible.
func (r *Runtime) consoleFunc(level slog.Level) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if r.opts.Log == nil {
			return goja.Undefined()
		}
		r.lines++
		switch {
		case r.lines == MaxLines+1:
			r.opts.Log(slog.LevelWarn, fmt.Sprintf("console: more than %d lines; the rest is dropped", MaxLines))
			return goja.Undefined()
		case r.lines > MaxLines:
			return goja.Undefined()
		}
		parts := make([]string, len(call.Arguments))
		for i, a := range call.Arguments {
			parts[i] = r.format(a)
		}
		r.opts.Log(level, cut(strings.Join(parts, " "), MaxLineBytes))
		return goja.Undefined()
	}
}

// format writes a value for the console.
func (r *Runtime) format(v goja.Value) string {
	obj, ok := v.(*goja.Object)
	if !ok {
		return v.String()
	}
	if _, fn := goja.AssertFunction(obj); fn || isError(obj) {
		return v.String()
	}
	stringify, _ := goja.AssertFunction(r.vm.Get("JSON").ToObject(r.vm).Get("stringify"))
	if s, err := stringify(goja.Undefined(), v); err == nil && !goja.IsUndefined(s) {
		return s.String()
	}
	return v.String()
}

// cut shortens s to at most n bytes at a character boundary, marked with "…".
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := n - len("…")
	for i > 0 && s[i]&0xC0 == 0x80 {
		i--
	}
	return s[:i] + "…"
}
