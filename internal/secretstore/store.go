// Package secretstore is the local secret store (ADR 0022, S39d): the source "local" of
// ${secret:local/NAME}. Values belong to one project and lie outside of it, in one file
// per project in the data folder of the account: %LOCALAPPDATA%\runnel\secrets for the
// user (elsewhere <config>/runnel/secrets). Each value is encrypted with AES-256-GCM; the
// associated data binds it to the project and its name, so that values cannot be swapped
// between names or projects. The key comes from RUNNEL_MASTER_KEY.
package secretstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
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
	"strings"
	"sync"
	"time"

	"github.com/runnelhq/runnel/internal/vars"
)

// Limits of the store.
const (
	// MaxValue is the largest value of a secret (e.g. a PFX file as base64).
	MaxValue = 1 << 20
	// maxFile is the largest file of a project that is read.
	maxFile = 32 << 20
	// fileVersion is the version of the file format.
	fileVersion = 1
)

// Store is the local secret store of one account: a folder with one file per project.
type Store struct {
	// Dir holds the files of the projects.
	Dir string
}

// Info describes a secret without its value.
type Info struct {
	Name    string
	Updated time.Time
}

// file is the format of the file of a project. Names and times are plain text; each value
// is encrypted on its own with a fresh nonce.
type file struct {
	Version int              `json:"version"`
	Project string           `json:"project"`
	KeyID   string           `json:"keyId,omitempty"`
	Secrets map[string]entry `json:"secrets"`
}

type entry struct {
	Nonce   []byte    `json:"nonce"`
	Value   []byte    `json:"value"`
	Updated time.Time `json:"updated"`
}

var (
	defaultMu    sync.Mutex
	defaultStore *Store
)

// SetDefault sets the store that Default returns (e.g. in tests); nil: the store of the
// user.
func SetDefault(s *Store) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultStore = s
}

// Default returns the store of the account the program runs as: that set with
// SetDefault, else the store of the user.
func Default() (*Store, error) {
	defaultMu.Lock()
	s := defaultStore
	defaultMu.Unlock()
	if s != nil {
		return s, nil
	}
	return User()
}

// User returns the store of the user: %LOCALAPPDATA%\runnel\secrets on Windows (not
// roaming), elsewhere <config>/runnel/secrets ($XDG_CONFIG_HOME or ~/.config).
func User() (*Store, error) {
	var base string
	if runtime.GOOS == "windows" {
		base = os.Getenv("LOCALAPPDATA")
	}
	if base == "" {
		var err error
		if base, err = os.UserConfigDir(); err != nil {
			return nil, fmt.Errorf("no folder for the local secret store: %w", err)
		}
	}
	return &Store{Dir: filepath.Join(base, "runnel", "secrets")}, nil
}

// projectKey is the absolute, cleaned project folder; on Windows in lower case.
func projectKey(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	key := filepath.Clean(abs)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key) // C:\Data and c:\data are the same folder
	}
	return key, nil
}

// path returns the file of a project: the name of its folder and 16 hex digits of a
// SHA-256 over its key (like the run history, ADR 0026).
func (s *Store) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	name := filepath.Base(key)
	if name == "" || name == "." || name == string(filepath.Separator) || strings.HasSuffix(name, ":") || strings.HasSuffix(name, `\`) {
		name = "project"
	}
	return filepath.Join(s.Dir, name+"-"+hex.EncodeToString(sum[:8])+".json")
}

// decodeFile reads the file of a project and checks its form.
func decodeFile(data []byte) (*file, error) {
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("not a file of the local secret store: %w", err)
	}
	if f.Version != fileVersion {
		return nil, fmt.Errorf("version %d of the local secret store is not supported (expected %d; written by another version of Runnel?)", f.Version, fileVersion)
	}
	if f.Project == "" {
		return nil, errors.New("not a file of the local secret store: no project")
	}
	for name, e := range f.Secrets {
		if !vars.ValidSecretName(name) {
			return nil, fmt.Errorf("invalid secret name %q in the local secret store", name)
		}
		if len(e.Nonce) != nonceSize || len(e.Value) < tagSize {
			return nil, fmt.Errorf("secret %q in the local secret store is damaged", name)
		}
	}
	if f.Secrets == nil {
		f.Secrets = map[string]entry{}
	}
	return &f, nil
}

// read returns the file of a project; a missing file has no secrets.
func (s *Store) read(key string) (*file, string, error) {
	path := s.path(key)
	empty := &file{Version: fileVersion, Project: key, Secrets: map[string]entry{}}
	fh, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return empty, path, nil
	}
	if err != nil {
		return nil, path, err
	}
	defer func() { _ = fh.Close() }()
	data, err := io.ReadAll(io.LimitReader(fh, maxFile+1))
	if err != nil {
		return nil, path, err
	}
	if len(data) > maxFile {
		return nil, path, fmt.Errorf("%s is larger than %d MB", path, maxFile>>20)
	}
	f, err := decodeFile(data)
	if err != nil {
		return nil, path, fmt.Errorf("%s: %w", path, err)
	}
	if f.Project != key {
		return nil, path, fmt.Errorf("%s belongs to the project %s, not %s", path, f.Project, key)
	}
	return f, path, nil
}

// write replaces the file of a project via a temporary file, so that a reader never sees
// half of it; only the account may read it (0600; on Windows the permissions of the
// folder, the user profile).
func write(path string, f *file) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".secrets-*.tmp")
	if err != nil {
		return err
	}
	_, err = tmp.Write(append(data, '\n'))
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

// List returns the secrets of a project, sorted by name; it needs no key.
func (s *Store) List(projectDir string) ([]Info, error) {
	key, err := projectKey(projectDir)
	if err != nil {
		return nil, err
	}
	f, _, err := s.read(key)
	if err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(f.Secrets))
	for name, e := range f.Secrets {
		out = append(out, Info{Name: name, Updated: e.Updated})
	}
	slices.SortFunc(out, func(a, b Info) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// Has reports whether a secret of a project is set; it needs no key.
func (s *Store) Has(projectDir, name string) (bool, error) {
	key, err := projectKey(projectDir)
	if err != nil {
		return false, err
	}
	f, _, err := s.read(key)
	if err != nil {
		return false, err
	}
	_, ok := f.Secrets[name]
	return ok, nil
}

// Get returns the value of a secret of a project; ok is false if it is not set. Errors
// never contain a value.
func (s *Store) Get(projectDir, name string) (value string, ok bool, err error) {
	key, err := projectKey(projectDir)
	if err != nil {
		return "", false, err
	}
	f, _, err := s.read(key)
	if err != nil {
		return "", false, err
	}
	e, ok := f.Secrets[name]
	if !ok {
		return "", false, nil
	}
	k, err := s.key()
	if err != nil {
		return "", false, err
	}
	if f.KeyID != k.id {
		return "", false, errOtherKey
	}
	gcm, err := newGCM(k.enc)
	if err != nil {
		return "", false, err
	}
	plain, err := gcm.Open(nil, e.Nonce, e.Value, aad(key, name))
	if err != nil {
		return "", false, fmt.Errorf("secret %q cannot be decrypted: the file of the local secret store was changed or damaged", name)
	}
	return string(plain), true, nil
}

var errOtherKey = errors.New("the secrets of this project were stored with another key (was RUNNEL_MASTER_KEY set, changed or removed, or are they from another account?)")

// Set stores the value of a secret of a project (replacing an earlier one).
func (s *Store) Set(projectDir, name, value string, now time.Time) error {
	if !vars.ValidSecretName(name) {
		return fmt.Errorf("invalid secret name %q (letters, digits, ., - and _)", name)
	}
	if len(value) > MaxValue {
		return fmt.Errorf("the value is larger than %d MB", MaxValue>>20)
	}
	key, err := projectKey(projectDir)
	if err != nil {
		return err
	}
	f, path, err := s.read(key)
	if err != nil {
		return err
	}
	k, err := s.key()
	if err != nil {
		return err
	}
	if len(f.Secrets) > 0 && f.KeyID != k.id {
		return errOtherKey
	}
	gcm, err := newGCM(k.enc)
	if err != nil {
		return err
	}
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	f.KeyID = k.id
	f.Secrets[name] = entry{Nonce: nonce, Value: gcm.Seal(nil, nonce, []byte(value), aad(key, name)), Updated: now.UTC()}
	return write(path, f)
}

// Delete removes a secret of a project; false if it was not set. It needs no key.
func (s *Store) Delete(projectDir, name string) (bool, error) {
	key, err := projectKey(projectDir)
	if err != nil {
		return false, err
	}
	f, path, err := s.read(key)
	if err != nil {
		return false, err
	}
	if _, ok := f.Secrets[name]; !ok {
		return false, nil
	}
	delete(f.Secrets, name)
	return true, write(path, f)
}

const (
	nonceSize = 12
	tagSize   = 16
)

// aad binds a value to the project and the name of the secret.
func aad(project, name string) []byte {
	return []byte("runnel secret v1\x00" + project + "\x00" + name)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
