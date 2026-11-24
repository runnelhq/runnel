// Package logfile writes a log file that is rotated by size (roadmap S41): when the
// file would grow beyond its limit, it is renamed to <name>.1, older ones move on to
// <name>.2 … up to the number of old files kept (S71), and a new file is started. The
// Windows service writes its server log with it; NewHandler formats its lines.
package logfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// DefaultMaxSize is the size from which a log file is rotated; DefaultKeep is the number
// of old files kept.
const (
	DefaultMaxSize = 10 << 20
	DefaultKeep    = 5
)

// File is a log file; it is safe for use by several goroutines.
type File struct {
	path string
	max  int64
	keep int

	mu   sync.Mutex
	f    *os.File
	size int64
}

// Open opens the log file at path for appending, creating it and its folder if
// needed. limit is the size from which it is rotated (DefaultMaxSize if 0), keep the
// number of old files kept (DefaultKeep if 0).
func Open(path string, limit int64, keep int) (*File, error) {
	if limit <= 0 {
		limit = DefaultMaxSize
	}
	if keep <= 0 {
		keep = DefaultKeep
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("cannot create the log folder: %w", err)
	}
	l := &File{path: path, max: limit, keep: keep}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *File) open() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("cannot open the log file: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("cannot open the log file: %w", err)
	}
	l.f, l.size = f, info.Size()
	return nil
}

// Write appends p, rotating the file first if p would make it larger than the limit.
// A single write is never split.
func (l *File) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return 0, os.ErrClosed
	}
	if l.size > 0 && l.size+int64(len(p)) > l.max {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

// rotate moves the old files on (<path>.1 to .2 …, the oldest is removed), renames the
// file to <path>.1 and starts a new one. Windows cannot rename an open file, so it is
// closed first.
func (l *File) rotate() error {
	if err := l.f.Close(); err != nil {
		return err
	}
	l.f = nil
	_ = os.Remove(fmt.Sprintf("%s.%d", l.path, l.keep))
	for i := l.keep - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", l.path, i), fmt.Sprintf("%s.%d", l.path, i+1))
	}
	old := l.path + ".1"
	if err := os.Rename(l.path, old); err != nil {
		// Keep writing to the full file rather than losing messages.
		_ = l.open()
		return fmt.Errorf("cannot rotate the log file: %w", err)
	}
	return l.open()
}

// Close closes the file.
func (l *File) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}
