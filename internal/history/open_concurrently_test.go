package history

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
)

// TestOpenNewConcurrently: processes that start at the same time (service, server, runnel run) all open a new
// history; the tables are created once.
func TestOpenNewConcurrently(t *testing.T) {
	dir := t.TempDir()
	const n = 4
	stores := make([]*Store, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { stores[i], errs[i] = Open(context.Background(), dir) })
	}
	wg.Wait()
	for i := range n {
		if errs[i] != nil {
			t.Errorf("open %d: %v", i, errs[i])
			continue
		}
		t.Cleanup(func() { _ = stores[i].Close() })
	}
	if t.Failed() {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), FileName) {
			t.Errorf("file %s left in the history folder", e.Name())
		}
	}
	putRunning(t, stores[0], "r")
	if list, err := stores[n-1].List(context.Background(), 10); err != nil || len(list) != 1 {
		t.Errorf("list %v %v", list, err)
	}
}
