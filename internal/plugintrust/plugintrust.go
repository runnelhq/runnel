// Package plugintrust keeps the approvals of plugins (ADR 0019, Nachtrag 2026-11-22):
// a plugin of a project starts only if it is approved on this computer for this project,
// with a SHA-256 over the content of its folder. Approvals never lie in the project (a
// project from elsewhere could bring them along): those of the user in the configuration
// folder, those of the Windows service in %ProgramData%\runnel\trust, writable only by
// administrators.
package plugintrust

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Limits of the folder of a plugin for the hash; beyond them it is not loaded.
const (
	MaxBytes = 1 << 30 // 1 GB
	MaxFiles = 10000
)

// State of the approval of a plugin.
type State string

// States of an approval.
const (
	Trusted    State = "trusted"
	Changed    State = "changed" // approved, but files of the plugin changed since
	NotTrusted State = "not trusted"
)

// Status is the approval of a plugin as Check finds it.
type Status struct {
	State State
	// Hash of the folder of the plugin now.
	Hash string
	// Service is set if the approval is that of the service.
	Service bool
}

// Entry is one approval in a file of approvals.
type Entry struct {
	Project string    `json:"project"`
	Plugin  string    `json:"plugin"`
	Version string    `json:"version,omitempty"`
	Hash    string    `json:"hash"`
	Trusted time.Time `json:"trusted"`
}

type fileFormat struct {
	Version int     `json:"version"`
	Plugins []Entry `json:"plugins"`
}

// Store names the files of approvals: that of the user and, on Windows, that of the service.
type Store struct {
	User    string
	Service string
}

// DefaultStore returns the files of approvals of this computer: <config>/runnel/trusted-plugins.json
// of the user (os.UserConfigDir, %AppData% on Windows) and serviceDir/plugins.json, if set.
func DefaultStore(serviceDir string) (Store, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return Store{}, fmt.Errorf("no configuration folder for the approvals of plugins: %w", err)
	}
	s := Store{User: filepath.Join(cfg, "runnel", "trusted-plugins.json")}
	if serviceDir != "" {
		s.Service = filepath.Join(serviceDir, "plugins.json")
	}
	return s, nil
}

// Hash returns the SHA-256 over the content of the folder of a plugin: the relative paths
// (with "/") and contents of all files, sorted. Links and other special files are
// refused, as are folders beyond MaxBytes or MaxFiles.
func Hash(dir string) (string, error) {
	// Within an os.Root, so that no link swapped in while hashing leads out of the folder.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	h := sha256.New()
	var files int
	var size int64
	err = fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is a link or a special file; plugins may only contain files and folders", rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if files++; files > MaxFiles {
			return fmt.Errorf("the plugin has more than %d files", MaxFiles)
		}
		if size += info.Size(); size > MaxBytes {
			return fmt.Errorf("the plugin is larger than %d MB", MaxBytes>>20)
		}
		f, err := root.Open(rel)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		// Path and length before the content, so that files cannot be cut differently.
		_, _ = io.WriteString(h, rel+"\x00"+strconv.FormatInt(info.Size(), 10)+"\x00")
		n, err := io.Copy(h, f)
		if err == nil && n != info.Size() {
			err = fmt.Errorf("%s changed while it was read", rel)
		}
		return err
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// projectKey is the absolute, cleaned project folder; on Windows compared without case.
func projectKey(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func sameProject(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// read returns the approvals of a file; a missing file has none.
func read(path string) ([]Entry, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%s cannot be read: %w", path, err)
	}
	return f.Plugins, nil
}

// write replaces a file of approvals (via a temporary file, so that a reader never sees half of it).
func write(path string, entries []Entry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(fileFormat{Version: 1, Plugins: entries}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".plugins-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Check returns the approval of the plugin name with the folder pluginDir in a project.
func (s Store) Check(projectDir, name, pluginDir string) (Status, error) {
	hash, err := Hash(pluginDir)
	if err != nil {
		return Status{}, err
	}
	key, err := projectKey(projectDir)
	if err != nil {
		return Status{}, err
	}
	st := Status{State: NotTrusted, Hash: hash}
	for _, f := range []struct {
		path    string
		service bool
	}{{s.User, false}, {s.Service, true}} {
		entries, err := read(f.path)
		if err != nil {
			return Status{}, err
		}
		for _, e := range entries {
			if e.Plugin != name || !sameProject(e.Project, key) {
				continue
			}
			if e.Hash == hash {
				return Status{State: Trusted, Hash: hash, Service: f.service}, nil
			}
			st.State = Changed
		}
	}
	return st, nil
}

// Trust approves a plugin with its current hash, for the user or (service) for the
// Windows service; an earlier approval of it is replaced.
func (s Store) Trust(projectDir, name, version, hash string, service bool, now time.Time) error {
	path, err := s.file(service)
	if err != nil {
		return err
	}
	key, err := projectKey(projectDir)
	if err != nil {
		return err
	}
	entries, err := read(path)
	if err != nil {
		return err
	}
	entries = slices.DeleteFunc(entries, func(e Entry) bool { return e.Plugin == name && sameProject(e.Project, key) })
	entries = append(entries, Entry{Project: key, Plugin: name, Version: version, Hash: hash, Trusted: now.UTC()})
	return write(path, entries)
}

// Untrust removes the approval of a plugin; false if there was none.
func (s Store) Untrust(projectDir, name string, service bool) (bool, error) {
	path, err := s.file(service)
	if err != nil {
		return false, err
	}
	key, err := projectKey(projectDir)
	if err != nil {
		return false, err
	}
	entries, err := read(path)
	if err != nil {
		return false, err
	}
	n := len(entries)
	entries = slices.DeleteFunc(entries, func(e Entry) bool { return e.Plugin == name && sameProject(e.Project, key) })
	if len(entries) == n {
		return false, nil
	}
	return true, write(path, entries)
}

func (s Store) file(service bool) (string, error) {
	if !service {
		return s.User, nil
	}
	if s.Service == "" {
		return "", errors.New("approvals for the service exist only on Windows")
	}
	return s.Service, nil
}
