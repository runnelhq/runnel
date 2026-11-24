package jsonrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// peer is the other side for the tests: it answers each request with fn.
func peer(t *testing.T, fn func(method string, params json.RawMessage) (any, *Error)) (*Conn, func(string)) {
	t.Helper()
	toPeer, fromHost := io.Pipe()
	toHost, fromPeer := io.Pipe()
	var mu sync.Mutex
	write := func(line string) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = fromPeer.Write([]byte(line + "\n"))
	}
	go func() {
		s := bufio.NewScanner(toPeer)
		for s.Scan() {
			var m message
			_ = json.Unmarshal(s.Bytes(), &m)
			if m.ID == nil {
				if m.Method == "$/cancelRequest" {
					write(`{"jsonrpc":"2.0","method":"cancelled","params":` + string(m.Params) + `}`)
				}
				continue
			}
			res, rpcErr := fn(m.Method, m.Params)
			raw, _ := json.Marshal(res)
			out, _ := json.Marshal(message{JSONRPC: "2.0", ID: m.ID, Result: raw, Error: rpcErr})
			write(string(out))
		}
	}()
	notes := make(chan string, 10)
	c := New(toHost, fromHost, func(method string, params json.RawMessage) { notes <- method + " " + string(params) })
	t.Cleanup(func() { _ = fromHost.Close(); _ = fromPeer.Close() })
	_ = notes
	return c, write
}

// Calls are matched with their answers by id, also when they run concurrently; errors
// of the other side come back as *Error.
func TestCall(t *testing.T) {
	c, _ := peer(t, func(method string, params json.RawMessage) (any, *Error) {
		if method == "fail" {
			return nil, &Error{Code: -32000, Message: "it failed"}
		}
		var p struct{ N int }
		_ = json.Unmarshal(params, &p)
		time.Sleep(time.Duration(5-p.N) * time.Millisecond) // later calls answer first
		return map[string]int{"double": 2 * p.N}, nil
	})
	var wg sync.WaitGroup
	for n := range 5 {
		wg.Go(func() {
			var res struct{ Double int }
			if err := c.Call(context.Background(), "double", map[string]int{"n": n}, &res); err != nil || res.Double != 2*n {
				t.Errorf("%d: %+v %v", n, res, err)
			}
		})
	}
	wg.Wait()
	err := c.Call(context.Background(), "fail", nil, nil)
	if rpcErr, ok := errors.AsType[*Error](err); !ok || rpcErr.Code != -32000 || rpcErr.Message != "it failed" {
		t.Errorf("fail: %v", err)
	}
}

// Requests of the other side are refused with method not found; notifications reach
// notify; a cancelled call sends $/cancelRequest and returns at once.
func TestNotifyAndCancel(t *testing.T) {
	toHost, fromPeer := io.Pipe()
	toPeer, fromHost := io.Pipe()
	notes := make(chan string, 4)
	c := New(toHost, fromHost, func(method string, params json.RawMessage) { notes <- method + " " + string(params) })
	defer func() { _ = fromPeer.Close() }()
	// Everything the host writes, line by line (io.Pipe has no buffer).
	lines := make(chan string, 10)
	go func() {
		s := bufio.NewScanner(toPeer)
		for s.Scan() {
			lines <- s.Text()
		}
	}()

	go func() {
		_, _ = fromPeer.Write([]byte(`{"jsonrpc":"2.0","method":"log","params":{"text":"hi"}}` + "\n"))
	}()
	if got := <-notes; got != `log {"text":"hi"}` {
		t.Errorf("notification %q", got)
	}
	go func() { _, _ = fromPeer.Write([]byte(`{"jsonrpc":"2.0","id":7,"method":"secrets.get"}` + "\n")) }()
	if l := <-lines; !strings.Contains(l, `"id":7,"error":{"code":-32601,"message":"method not found: secrets.get"}`) {
		t.Errorf("request of the peer: %s", l)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- c.Call(ctx, "slow", nil, nil) }()
	if l := <-lines; !strings.Contains(l, `"method":"slow"`) {
		t.Fatalf("request: %s", l)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("cancel: %v", err)
	}
	if l := <-lines; !strings.Contains(l, `"method":"$/cancelRequest","params":{"id":1}`) {
		t.Errorf("cancel request: %s", l)
	}
}

// Invalid messages, too long lines and the end of the stream end the connection; open
// and later calls get the reason.
func TestEnd(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{"not json\n", "invalid message (not JSON-RPC 2.0): not json"},
		{`{"jsonrpc":"1.0","id":1}` + "\n", "invalid message"},
		{strings.Repeat("x", MaxMessage+1) + "\n", "message longer than 16 MiB"},
		{"", "connection closed"},
	} {
		c := New(strings.NewReader(tt.input), io.Discard, nil)
		<-c.Done()
		if err := c.Err(); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%.20q: %v", tt.input, err)
		}
		if err := c.Call(context.Background(), "x", nil, nil); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("call after end: %v", err)
		}
	}
}

// FuzzRead checks that no input from a plugin makes the reader panic or hang.
func FuzzRead(f *testing.F) {
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"result":{"a":1}}` + "\n"))
	f.Add([]byte(`{"jsonrpc":"2.0","method":"x","params":[1,2]}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"y"}`))
	f.Add([]byte(`{"jsonrpc":"2.0","id":null,"error":{"code":"x"}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		c := New(strings.NewReader(string(data)), io.Discard, func(string, json.RawMessage) {})
		select {
		case <-c.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("reader did not end")
		}
	})
}
