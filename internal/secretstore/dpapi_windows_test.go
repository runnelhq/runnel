package secretstore

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Without RUNNEL_MASTER_KEY, Windows protects a random key with DPAPI; it is made once and
// reused, and the store reads back what it wrote.
func TestDPAPI(t *testing.T) {
	t.Setenv(MasterKeyEnv, "")
	for _, machine := range []bool{false, true} {
		s := &Store{Dir: filepath.Join(t.TempDir(), "secrets"), Machine: machine}
		project := t.TempDir()
		if err := s.Set(project, "pw", "Pa55word!", now); err != nil {
			t.Fatal(err)
		}
		blob, err := os.ReadFile(filepath.Join(s.Dir, keyFile))
		if err != nil || len(blob) < 32 {
			t.Fatalf("key file: %v", err)
		}
		if err := s.Set(project, "other", "x", now); err != nil {
			t.Fatal(err)
		}
		again, _ := os.ReadFile(filepath.Join(s.Dir, keyFile))
		if !bytes.Equal(blob, again) {
			t.Error("the key was made again")
		}
		if v, ok, err := s.Get(project, "pw"); err != nil || !ok || v != "Pa55word!" {
			t.Errorf("machine %v: %q %v %v", machine, v, ok, err)
		}
		// A damaged key file is reported.
		_ = os.WriteFile(filepath.Join(s.Dir, keyFile), []byte("damaged"), 0o600)
		if _, _, err := s.Get(project, "pw"); err == nil {
			t.Error("damaged key file accepted")
		}
	}
}
