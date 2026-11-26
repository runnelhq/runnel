package transfer

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

// waitResult is the result of transfer.wait running in the background.
type waitResult struct {
	res  action.Result
	logs []string
	err  error
}

func waitAsync(ctx context.Context, t *testing.T, srv *testServer, with map[string]any) chan waitResult {
	t.Helper()
	done := make(chan waitResult, 1)
	go func() {
		res, logs, err := execute(ctx, t, waitDefinition, srv.conn(), with)
		done <- waitResult{res, logs, err}
	}()
	return done
}

func TestWait(t *testing.T) {
	eachServer(t, testWait)
}

func testWait(t *testing.T, srv *testServer) {
	done := waitAsync(context.Background(), t, srv, map[string]any{"path": "in/*.csv", "interval": "20ms", "stableFor": "100ms"})
	time.Sleep(100 * time.Millisecond)
	writeFile(t, srv.root, "in/a.csv", "a")
	writeFile(t, srv.root, "in/notes.txt", "not matching")
	time.Sleep(50 * time.Millisecond)
	writeFile(t, srv.root, "in/a.csv", "growing")
	r := <-done
	if r.err != nil || !reflect.DeepEqual(r.res.Output, []any{"in/a.csv"}) {
		t.Fatalf("%v %v", r.res.Output, r.err)
	}
	if srv.connections() != 1 {
		t.Errorf("%d connections, want one for the whole wait", srv.connections())
	}
	// A file that is there already; folders do not count.
	writeFile(t, srv.root, "in/x.csv/y", "")
	res, _, err := execute(context.Background(), t, waitDefinition, srv.conn(), map[string]any{"path": "in/*.csv"})
	if err != nil || !reflect.DeepEqual(res.Output, []any{"in/a.csv"}) {
		t.Errorf("%v %v", res.Output, err)
	}
}

func TestWaitMaxWait(t *testing.T) {
	eachServer(t, testWaitMaxWait)
}

func testWaitMaxWait(t *testing.T, srv *testServer) {
	start := time.Now()
	_, _, err := execute(context.Background(), t, waitDefinition, srv.conn(), map[string]any{"path": "in/*.csv", "interval": "20ms", "maxWait": "100ms"})
	if err == nil || err.Error() != `no file matching "in/*.csv" on the server after 100ms` {
		t.Errorf("%v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("took %s", time.Since(start))
	}
	// Still changing at the end.
	writeFile(t, srv.root, "in/a.csv", "a")
	_, _, err = execute(context.Background(), t, waitDefinition, srv.conn(), map[string]any{"path": "in/a.csv", "interval": "20ms", "maxWait": "100ms", "stableFor": "1h"})
	if err == nil || err.Error() != `files matching "in/a.csv" on the server still changed after 100ms` {
		t.Errorf("%v", err)
	}
}

// A dropped connection is opened again; the wait goes on.
func TestWaitReconnect(t *testing.T) {
	eachServer(t, testWaitReconnect)
}

func testWaitReconnect(t *testing.T, srv *testServer) {
	done := waitAsync(context.Background(), t, srv, map[string]any{"path": "in/a.csv", "interval": "20ms"})
	time.Sleep(100 * time.Millisecond)
	srv.drop()
	time.Sleep(100 * time.Millisecond)
	writeFile(t, srv.root, "in/a.csv", "a")
	r := <-done
	if r.err != nil || !reflect.DeepEqual(r.res.Output, []any{"in/a.csv"}) {
		t.Fatalf("%v %v", r.res.Output, r.err)
	}
	if srv.connections() < 2 {
		t.Errorf("%d connections", srv.connections())
	}
	lost := slices.ContainsFunc(r.logs, func(l string) bool { return strings.HasPrefix(l, "lost the connection to 127.0.0.1:"+srv.port) })
	if !lost || !slices.Contains(r.logs, "connected again to 127.0.0.1:"+srv.port) {
		t.Errorf("%v", r.logs)
	}
}

func TestWaitCancel(t *testing.T) {
	eachServer(t, testWaitCancel)
}

func testWaitCancel(t *testing.T, srv *testServer) {
	ctx, cancel := context.WithCancel(context.Background())
	done := waitAsync(ctx, t, srv, map[string]any{"path": "in/a.csv", "interval": "20ms"})
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case r := <-done:
		if !errors.Is(r.err, context.Canceled) {
			t.Errorf("%v", r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the wait did not end")
	}
}

func TestWaitErrors(t *testing.T) {
	eachServer(t, testWaitErrors)
}

func testWaitErrors(t *testing.T, srv *testServer) {
	cases := []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"path": "a", "interval": "1ms"}, "interval 1ms is too short (at least 10ms)"},
		{map[string]any{"path": "a", "maxWait": "soon"}, `maxWait: invalid duration "soon"`},
		{map[string]any{"path": "a", "stableFor": "x"}, `stableFor: "x" is not a duration`},
		{map[string]any{"path": "a", "timeout": "-1s"}, `timeout: "-1s" is not a duration`},
		{map[string]any{"path": "in/["}, `invalid wildcard pattern "["`},
	}
	for _, c := range cases {
		if _, _, err := execute(context.Background(), t, waitDefinition, srv.conn(), c.with); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.with, err, c.want)
		}
	}
	actiontest.ConfigError(t, waitDefinition, map[string]any{"connection": "s"}, `missing required field "path"`)
	// A refused host key or sign-in ends the wait instead of trying again.
	if !refused(&hostKeyError{"x"}) || !refused(errors.Join(errors.New("connection sftp"), &signInError{"y"})) || refused(errors.New("cannot connect")) {
		t.Error("refused")
	}
}
