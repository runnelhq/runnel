package secretstore

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// MasterKeyEnv is the environment variable with the key of the store: 32 random bytes in
// base64. If it is set, it is used instead of DPAPI (on Windows too, e.g. in a container).
const MasterKeyEnv = "RUNNEL_MASTER_KEY"

// keyFile is the file with the key protected by DPAPI, one per store.
const keyFile = "key.dpapi"

// keys are the keys derived from the key of the store: one encrypts, the other names it
// in the files (keyId), so that a different key is reported as such.
type keys struct {
	enc []byte
	id  string
}

func derive(master []byte) keys {
	mac := func(label string) []byte {
		h := hmac.New(sha256.New, master)
		h.Write([]byte("runnel secret store v1: " + label))
		return h.Sum(nil)
	}
	return keys{enc: mac("encryption"), id: hex.EncodeToString(mac("key id")[:8])}
}

// ErrNoKey is returned on systems without DPAPI when RUNNEL_MASTER_KEY is not set.
var ErrNoKey = errors.New("the local secret store needs a key on this system: set " + MasterKeyEnv +
	" to 32 random bytes in base64 (e.g. from \"openssl rand -base64 32\")")

// ParseMasterKey reads the value of RUNNEL_MASTER_KEY: exactly 32 bytes in base64
// (standard or URL alphabet, with or without padding). The error never contains the value.
func ParseMasterKey(text string) ([]byte, error) {
	text = strings.TrimSpace(text)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if k, err := enc.DecodeString(text); err == nil {
			if len(k) != 32 {
				return nil, fmt.Errorf("%s must be 32 bytes in base64, not %d (e.g. from \"openssl rand -base64 32\")", MasterKeyEnv, len(k))
			}
			if weakKey(k) {
				return nil, fmt.Errorf("%s is not random (all bytes are the same); create one with \"openssl rand -base64 32\"", MasterKeyEnv)
			}
			return k, nil
		}
	}
	return nil, fmt.Errorf("%s is not valid base64; it must be 32 random bytes in base64 (e.g. from \"openssl rand -base64 32\")", MasterKeyEnv)
}

// weakKey reports keys made of one repeated byte (e.g. all zeros), a sign of a placeholder.
func weakKey(k []byte) bool {
	for _, b := range k[1:] {
		if b != k[0] {
			return false
		}
	}
	return true
}

// key returns the keys of the store: from RUNNEL_MASTER_KEY, else from the key file
// protected by DPAPI, which create makes on first use.
func (s *Store) key(create bool) (keys, error) {
	if text := os.Getenv(MasterKeyEnv); text != "" {
		k, err := ParseMasterKey(text)
		if err != nil {
			return keys{}, err
		}
		return derive(k), nil
	}
	if !dpapiAvailable {
		return keys{}, ErrNoKey
	}
	path := filepath.Join(s.Dir, keyFile)
	blob, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) && create {
		return s.createKey(path)
	}
	if err != nil {
		return keys{}, fmt.Errorf("cannot read the key of the local secret store: %w", err)
	}
	k, err := unprotect(blob, s.Machine)
	if err == nil && len(k) != 32 {
		err = errors.New("not a key")
	}
	if err != nil {
		return keys{}, fmt.Errorf("cannot decrypt the key of the local secret store %s (DPAPI; was it made by another account or on another computer?): %w", path, err)
	}
	return derive(k), nil
}

// createKey makes a random key, protects it with DPAPI and stores it; if another process
// stored one meanwhile, that one is used.
func (s *Store) createKey(path string) (keys, error) {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return keys{}, err
	}
	blob, err := protect(k, s.Machine)
	if err != nil {
		return keys{}, fmt.Errorf("cannot protect the key of the local secret store (DPAPI): %w", err)
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return keys{}, err
	}
	tmp, err := os.CreateTemp(s.Dir, ".key-*.tmp")
	if err != nil {
		return keys{}, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, err = tmp.Write(blob)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return keys{}, err
	}
	// A link fails if the file exists: two processes never replace each other's key.
	if err := os.Link(tmp.Name(), path); errors.Is(err, fs.ErrExist) {
		return s.key(false)
	} else if err != nil {
		return keys{}, err
	}
	return derive(k), nil
}
