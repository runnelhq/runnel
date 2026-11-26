package script

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/dop251/goja"

	"github.com/runnelhq/runnel/internal/record"
)

// ErrTimeLimit is wrapped by the error of a call that took longer than the limit of its
// session.
var ErrTimeLimit = errors.New("the script took longer than allowed")

// Session runs code of a runtime many times, e.g. once per record of the transform
// core.script (ADR 0028): console lines are counted for the whole session, a limited
// call may take at most the limit of the session, and cancelling the context interrupts
// the running call. After an interrupt the runtime can be used for the next call. Like
// its runtime, a session belongs to one goroutine; Close ends it.
type Session struct {
	r     *Runtime
	ctx   context.Context
	limit time.Duration
	timer *time.Timer
	stop  func() bool
	args  []goja.Value
	out   []record.Record

	mu       sync.Mutex // guards the fields below against the timer and the context
	running  bool
	deadline time.Time
	expired  bool
}

// Start begins a session; limit is the longest time of a limited call (0: none).
func (r *Runtime) Start(ctx context.Context, limit time.Duration) *Session {
	r.lines = 0
	s := &Session{r: r, ctx: ctx, limit: limit}
	s.stop = context.AfterFunc(ctx, func() { s.interrupt(false) })
	if limit > 0 {
		s.timer = time.AfterFunc(limit, func() { s.interrupt(true) })
		s.timer.Stop()
	}
	return s
}

// Close ends the session.
func (s *Session) Close() {
	s.stop()
	if s.timer != nil {
		s.timer.Stop()
	}
}

// Set makes a Go value (see Value) a global variable of the runtime, e.g. params.
func (s *Session) Set(name string, v any) {
	_ = s.r.vm.Set(name, s.r.Value(v))
}

// interrupt stops the running call: on timeout only if its deadline has passed (a timer
// of an earlier call may fire late).
func (s *Session) interrupt(timeout bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || timeout && time.Now().Before(s.deadline) {
		return
	}
	s.expired = timeout
	s.r.vm.Interrupt(ErrTimeLimit)
}

// Run runs code at the top level of the runtime, without limit: its variables and
// functions are global, seen by the functions called later (e.g. init of core.script).
func (s *Session) Run(name, code string) error {
	prg, err := goja.Compile(name, code, false)
	if err != nil {
		return syntaxError(name, err)
	}
	_, err = s.run(name, false, func() (goja.Value, error) { return s.r.vm.RunProgram(prg) })
	return err
}

// Records calls f with Go values as arguments (see Value) and returns its result as
// records: an object is one record (its properties in their order, values as by
// Export), a list of objects several, null and undefined none. limited applies the
// limit of the session. The records are valid until the next call.
func (s *Session) Records(f *Function, limited bool, args ...any) ([]record.Record, error) {
	s.args = s.args[:0]
	for _, a := range args {
		s.args = append(s.args, s.r.Value(a))
	}
	v, err := s.run(f.name, limited, func() (goja.Value, error) { return f.fn(goja.Undefined(), s.args...) })
	if err != nil {
		return nil, err
	}
	s.out = s.out[:0]
	if obj, ok := v.(*goja.Object); ok && obj.ClassName() == "Array" {
		n := obj.Get("length").ToInteger()
		for i := range n {
			rec, err := s.r.record(obj.Get(strconv.FormatInt(i, 10)))
			if err != nil {
				return nil, fmt.Errorf("%s: item %d of the returned list: %w", f.name, i, err)
			}
			s.out = append(s.out, rec)
		}
		return s.out, nil
	}
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return s.out, nil
	}
	rec, err := s.r.record(v)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.name, err)
	}
	return append(s.out, rec), nil
}

// run runs f as one call of the session; errors are those of Call, ErrTimeLimit after
// the limit.
func (s *Session) run(name string, limited bool, f func() (goja.Value, error)) (goja.Value, error) {
	timer := limited && s.timer != nil
	s.mu.Lock()
	s.running, s.expired = true, false
	if timer {
		s.deadline = time.Now().Add(s.limit)
	}
	s.mu.Unlock()
	if timer {
		s.timer.Reset(s.limit)
	}
	var v goja.Value
	err := s.ctx.Err() // checked after running is set: a cancel now interrupts f
	if err == nil {
		v, err = f()
	}
	if timer {
		s.timer.Stop()
	}
	s.mu.Lock()
	s.running = false
	expired := s.expired
	s.mu.Unlock()
	s.r.vm.ClearInterrupt() // no interrupt can come any more: running is false
	switch {
	case err == nil:
		return v, nil
	case s.ctx.Err() != nil:
		return nil, s.ctx.Err()
	case expired:
		return nil, fmt.Errorf("%s: %w", name, ErrTimeLimit)
	}
	return nil, s.r.failure(name, err)
}

// record converts an object to a record.
func (r *Runtime) record(v goja.Value) (record.Record, error) {
	obj, ok := v.(*goja.Object)
	if !ok || obj.ClassName() != "Object" || obj.ExportType() == typeBytes || obj.ExportType() == typeArrayBuffer {
		return record.Record{}, fmt.Errorf("return an object, a list of objects or null, not %s", kind(v))
	}
	keys := obj.Keys()
	fields := make([]record.Field, 0, len(keys))
	size := 0
	for _, k := range keys {
		x := obj.Get(k)
		if _, fn := goja.AssertFunction(x); fn {
			continue // like JSON.stringify: methods are left out
		}
		val, err := r.export(x, 1, &size)
		if err != nil {
			return record.Record{}, fmt.Errorf("field %q: %w", k, err)
		}
		fields = append(fields, record.Field{Name: k, Value: val})
	}
	return record.FromFields(fields), nil
}

// kind names the type of a value for messages.
func kind(v goja.Value) string {
	obj, ok := v.(*goja.Object)
	switch {
	case v == nil || goja.IsUndefined(v) || goja.IsNull(v):
		return "null"
	case !ok:
		switch v.ExportType().Kind().String() {
		case "string":
			return "a text"
		case "bool":
			return "a boolean"
		}
		return "a number"
	case obj.ClassName() == "Array":
		return "a list"
	case obj.ClassName() == "Object":
		return "binary data"
	}
	if _, fn := goja.AssertFunction(obj); fn {
		return "a function"
	}
	return "a " + obj.ClassName()
}
