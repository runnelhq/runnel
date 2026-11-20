package file

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

func TestWaitFindsFileThatAppears(t *testing.T) {
	dir := t.TempDir()
	go func() {
		time.Sleep(100 * time.Millisecond)
		tree(t, dir, map[string]string{"orders.csv": "1"})
	}()
	got, err := run(t, waitDefinition, map[string]any{"path": filepath.Join(dir, "*.csv"), "interval": "20ms", "maxWait": "5s"})
	if err != nil || names(got) != "orders.csv" {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestWaitForStableSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "growing.csv")
	tree(t, dir, map[string]string{"growing.csv": "1"})
	stop := make(chan struct{})
	go func() { // the file grows for 300 ms
		for i := 0; i < 6; i++ {
			time.Sleep(50 * time.Millisecond)
			f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			_, _ = f.WriteString("more")
			_ = f.Close()
		}
		close(stop)
	}()
	start := time.Now()
	if _, err := run(t, waitDefinition, map[string]any{"path": path, "interval": "20ms", "stableFor": "200ms"}); err != nil {
		t.Fatal(err)
	}
	<-stop
	if time.Since(start) < 450*time.Millisecond {
		t.Errorf("returned after %s, before the file stopped growing", time.Since(start))
	}
}

func TestWaitGivesUp(t *testing.T) {
	dir := t.TempDir()
	_, err := run(t, waitDefinition, map[string]any{"path": filepath.Join(dir, "*.csv"), "interval": "10ms", "maxWait": "50ms"})
	if err == nil || !strings.Contains(err.Error(), "no file matching") || !strings.Contains(err.Error(), "after 50ms") {
		t.Errorf("err = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	a := actiontest.New(t, waitDefinition, map[string]any{"path": filepath.Join(dir, "x"), "interval": "10ms"})
	if _, _, err := actiontest.Execute(ctx, a); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("cancelled: %v", err)
	}
}

func TestWaitConfig(t *testing.T) {
	actiontest.ConfigError(t, waitDefinition, nil, `missing required field "path"`)
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"path": "x", "interval": "soon"}, `interval: invalid duration "soon"`},
		{map[string]any{"path": "x", "interval": "1ms"}, "too short"},
		{map[string]any{"path": "x", "maxWait": "x"}, "maxWait: invalid duration"},
		{map[string]any{"path": "x", "stableFor": "x"}, "stableFor: invalid duration"},
	} {
		if _, err := run(t, waitDefinition, tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want containing %q", tt.with, err, tt.want)
		}
	}
}
