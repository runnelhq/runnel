package logfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRotate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "runnel.log")
	l, err := Open(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"first line\n", "second\n", "third line\n", "fourth\n"} {
		if _, err := l.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	// 11 + 7 = 18 bytes fit into 20; "third line" would exceed them and starts a new file.
	if got := read(t, path); got != "third line\nfourth\n" {
		t.Errorf("current %q", got)
	}
	if got := read(t, path+".1"); got != "first line\nsecond\n" {
		t.Errorf("old %q", got)
	}

	// Reopening appends and counts the existing size; the older .1 is replaced.
	l, err = Open(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Write([]byte("fifth\n")); err != nil {
		t.Fatal(err)
	}
	_ = l.Close()
	if got := read(t, path); got != "fifth\n" || read(t, path+".1") != "third line\nfourth\n" {
		t.Errorf("after reopening: %q, old %q", got, read(t, path+".1"))
	}
	// A line longer than the limit is written whole.
	l, _ = Open(path, 4)
	_, _ = l.Write([]byte(strings.Repeat("x", 10)))
	_ = l.Close()
	if got := read(t, path); got != strings.Repeat("x", 10) {
		t.Errorf("long line %q", got)
	}
	if _, err := l.Write([]byte("x")); err == nil {
		t.Error("write after close")
	}
}
