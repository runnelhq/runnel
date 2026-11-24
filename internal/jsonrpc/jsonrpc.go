// Package jsonrpc speaks JSON-RPC 2.0 over a pair of streams, one message per line
// (NDJSON): the transport of plugins (ADR 0019) and later of the MCP server (S53).
package jsonrpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// MaxMessage is the longest message (one line) accepted; a longer one ends the connection.
const MaxMessage = 16 << 20

// Error codes of JSON-RPC 2.0 and LSP used by Runnel.
const (
	CodeMethodNotFound   = -32601
	CodeInvalidParams    = -32602
	CodeInternalError    = -32603
	CodeRequestCancelled = -32800
)

// Error is an error answer of the other side.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// ErrClosed is returned by calls after the connection ended.
var ErrClosed = errors.New("connection closed")

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Conn is one side of a connection. Calls may run concurrently; answers are matched by id.
type Conn struct {
	wmu sync.Mutex
	w   io.Writer

	next    atomic.Int64
	mu      sync.Mutex
	pending map[int64]chan message
	err     error
	done    chan struct{}

	// notify receives notifications of the other side; nil drops them.
	notify func(method string, params json.RawMessage)
}

// New starts reading answers from r; requests go to w. notify (may be nil) receives the
// notifications of the other side, in order, on the reading goroutine.
func New(r io.Reader, w io.Writer, notify func(method string, params json.RawMessage)) *Conn {
	c := &Conn{w: w, pending: map[int64]chan message{}, done: make(chan struct{}), notify: notify}
	go c.read(r)
	return c
}

// Done is closed when the connection ended (the other side closed it or sent something
// invalid); Err tells why.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err returns why the connection ended; nil while it is open.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *Conn) write(m message) error {
	m.JSONRPC = "2.0"
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.w.Write(append(data, '\n'))
	return err
}

// Call sends a request and waits for its answer, decoded into result (may be nil). When
// ctx ends first, the other side gets "$/cancelRequest" with the id and Call returns the
// error of ctx without waiting.
func (c *Conn) Call(ctx context.Context, method string, params, result any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	id := c.next.Add(1)
	ch := make(chan message, 1)
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return c.err
	}
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	if err := c.write(message{ID: &id, Method: method, Params: raw}); err != nil {
		return fmt.Errorf("cannot send %s: %w", method, err)
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return m.Error
		}
		if result == nil || len(m.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(m.Result, result); err != nil {
			return fmt.Errorf("invalid answer to %s: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		// Not waiting for it: a plugin that does not read must not hold up the caller.
		go func() { _ = c.Notify("$/cancelRequest", map[string]int64{"id": id}) }()
		return ctx.Err()
	case <-c.done:
		return c.Err()
	}
}

// Notify sends a notification (no answer).
func (c *Conn) Notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.write(message{Method: method, Params: raw})
}

func (c *Conn) read(r io.Reader) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64<<10), MaxMessage)
	var err error
	for s.Scan() {
		line := bytes.TrimSpace(s.Bytes())
		if len(line) == 0 {
			continue
		}
		var m message
		if e := json.Unmarshal(line, &m); e != nil || m.JSONRPC != "2.0" {
			err = fmt.Errorf("invalid message (not JSON-RPC 2.0): %.200s", line)
			break
		}
		switch {
		case m.Method != "" && m.ID == nil:
			if c.notify != nil {
				c.notify(m.Method, m.Params)
			}
		case m.Method != "":
			// The host offers no methods to the other side (ADR 0019).
			_ = c.write(message{ID: m.ID, Error: &Error{Code: CodeMethodNotFound, Message: "method not found: " + m.Method}})
		case m.ID != nil:
			c.mu.Lock()
			ch := c.pending[*m.ID]
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
	}
	switch {
	case err != nil:
	case errors.Is(s.Err(), bufio.ErrTooLong):
		err = fmt.Errorf("message longer than %d MiB", MaxMessage>>20)
	case s.Err() != nil:
		err = s.Err()
	default:
		err = ErrClosed
	}
	c.mu.Lock()
	c.err = err
	c.mu.Unlock()
	close(c.done)
}
