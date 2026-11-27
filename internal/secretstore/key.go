package secretstore

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// MasterKeyEnv is the environment variable with the key of the store: 32 random bytes in
// base64.
const MasterKeyEnv = "RUNNEL_MASTER_KEY"

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

// ErrNoKey is returned when RUNNEL_MASTER_KEY is not set.
var ErrNoKey = errors.New("the local secret store needs a key: set " + MasterKeyEnv +
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

// key returns the keys of the store, derived from RUNNEL_MASTER_KEY.
func (s *Store) key() (keys, error) {
	text := os.Getenv(MasterKeyEnv)
	if text == "" {
		return keys{}, ErrNoKey
	}
	k, err := ParseMasterKey(text)
	if err != nil {
		return keys{}, err
	}
	return derive(k), nil
}
