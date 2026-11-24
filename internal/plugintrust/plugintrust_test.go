package plugintrust

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The hash covers names and contents of all files of the folder, in a fixed order.
func TestHash(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "plugin.yaml"), "name: iban\n")
	writeFile(t, filepath.Join(dir, "bin", "iban.exe"), "binary")
	h1, err := Hash(dir)
	if err != nil || !strings.HasPrefix(h1, "sha256:") || len(h1) != len("sha256:")+64 {
		t.Fatalf("%q %v", h1, err)
	}
	if h2, _ := Hash(dir); h2 != h1 {
		t.Error("hash not stable")
	}
	for _, change := range []func(){
		func() { writeFile(t, filepath.Join(dir, "bin", "iban.exe"), "binarY") },            // content
		func() { writeFile(t, filepath.Join(dir, "main.py"), "") },                          // new empty file
		func() { _ = os.Rename(filepath.Join(dir, "main.py"), filepath.Join(dir, "x.py")) }, // name
	} {
		change()
		h, err := Hash(dir)
		if err != nil || h == h1 {
			t.Errorf("hash did not change: %v", err)
		}
		h1 = h
	}
	if runtime.GOOS != "windows" { // links need extra rights on Windows
		if err := os.Symlink(filepath.Join(dir, "x.py"), filepath.Join(dir, "link.py")); err != nil {
			t.Fatal(err)
		}
		if _, err := Hash(dir); err == nil || !strings.Contains(err.Error(), "link.py is a link") {
			t.Errorf("link: %v", err)
		}
	}
}

// A plugin is trusted with the hash it had when approved; a changed file voids the
// approval; untrust removes it. Approvals of the service count as well (ADR 0019).
func TestTrust(t *testing.T) {
	base := t.TempDir()
	project := filepath.Join(base, "project")
	plugin := filepath.Join(project, "plugins", "iban")
	writeFile(t, filepath.Join(plugin, "plugin.yaml"), "name: iban\n")
	s := Store{User: filepath.Join(base, "user", "trusted-plugins.json"), Service: filepath.Join(base, "service", "plugins.json")}
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

	st, err := s.Check(project, "iban", plugin)
	if err != nil || st.State != NotTrusted {
		t.Fatalf("new: %+v %v", st, err)
	}
	if err := s.Trust(project, "iban", "1.0.0", st.Hash, false, now); err != nil {
		t.Fatal(err)
	}
	// The same project given another way (relative path with ..) is the same project.
	if st, _ := s.Check(filepath.Join(project, "plugins", ".."), "iban", plugin); st.State != Trusted || st.Service {
		t.Errorf("trusted: %+v", st)
	}
	// Another project with a plugin of the same name is not approved by it.
	other := filepath.Join(base, "other")
	writeFile(t, filepath.Join(other, "plugins", "iban", "plugin.yaml"), "name: iban\n")
	if st, _ := s.Check(other, "iban", filepath.Join(other, "plugins", "iban")); st.State != NotTrusted {
		t.Errorf("other project: %+v", st)
	}

	writeFile(t, filepath.Join(plugin, "plugin.yaml"), "name: iban\nexecutable: evil.exe\n")
	st, _ = s.Check(project, "iban", plugin)
	if st.State != Changed {
		t.Errorf("changed: %+v", st)
	}
	// Approved again for the service only: trusted through the file of the service.
	if err := s.Trust(project, "iban", "1.0.1", st.Hash, true, now); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.Check(project, "iban", plugin); st.State != Trusted || !st.Service {
		t.Errorf("service: %+v", st)
	}
	data, _ := os.ReadFile(s.Service)
	if !strings.Contains(string(data), `"version": "1.0.1"`) || !strings.Contains(string(data), `"trusted": "2026-11-24T12:00:00Z"`) {
		t.Errorf("service file:\n%s", data)
	}

	if ok, err := s.Untrust(project, "iban", true); !ok || err != nil {
		t.Errorf("untrust: %v %v", ok, err)
	}
	if ok, _ := s.Untrust(project, "iban", true); ok {
		t.Error("untrust twice")
	}
	if st, _ := s.Check(project, "iban", plugin); st.State != Changed { // the old approval of the user remains
		t.Errorf("after untrust: %+v", st)
	}
	if err := (Store{User: s.User}).Trust(project, "iban", "1", "h", true, now); err == nil || !strings.Contains(err.Error(), "only on Windows") {
		t.Errorf("service without file: %v", err)
	}

	// A broken file of approvals is an error, not "not trusted".
	writeFile(t, s.User, "{")
	if _, err := s.Check(project, "iban", plugin); err == nil || !strings.Contains(err.Error(), "cannot be read") {
		t.Errorf("broken file: %v", err)
	}
}

// Folders beyond the limits are refused.
func TestHashLimits(t *testing.T) {
	dir := t.TempDir()
	for i := range MaxFiles + 1 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%05d", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Hash(dir); err == nil || !strings.Contains(err.Error(), "more than 10000 files") {
		t.Errorf("files: %v", err)
	}
}
