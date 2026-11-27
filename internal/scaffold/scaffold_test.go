package scaffold

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/model"
)

var update = flag.Bool("update", false, "update golden files")

// TestFilesGolden: the content of the files of a new project.
func TestFilesGolden(t *testing.T) {
	var b strings.Builder
	for _, f := range Files("demo") {
		b.WriteString("=== " + f.Path + "\n" + f.Content)
	}
	golden := filepath.Join("testdata", "demo.golden")
	if *update {
		if err := os.WriteFile(golden, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if b.String() != string(want) {
		t.Errorf("files differ from %s (run go test ./internal/scaffold -update):\n%s", golden, b.String())
	}
}

func TestCreate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "My Project")
	res, err := Create(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Name != "my-project" {
		t.Errorf("name = %q, want my-project", res.Name)
	}
	var want []string
	for _, f := range Files(res.Name) {
		want = append(want, f.Path)
		p := filepath.Join(dir, filepath.FromSlash(f.Path))
		if strings.HasSuffix(f.Path, "/") {
			p = filepath.Join(p, gitKeep)
		}
		got, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("%s: %v", f.Path, err)
			continue
		}
		if string(got) != f.Content {
			t.Errorf("%s = %q, want %q", f.Path, got, f.Content)
		}
	}
	if !slices.Equal(res.Created, want) || len(res.Skipped) > 0 {
		t.Errorf("created %v, skipped %v; want created %v", res.Created, res.Skipped, want)
	}
	if _, err := os.Stat(filepath.Join(dir, WorkflowsDir, gitKeep)); err == nil {
		t.Error("workflows/ has the example workflow and needs no .gitkeep")
	}
	p, err := model.LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "my-project" || p.Environments[Environment] == nil {
		t.Errorf("project %q, environments %v", p.Name, p.Environments)
	}
}

func TestCreateNotEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(dir, Options{}); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("err = %v, want ErrNotEmpty", err)
	}
	if _, err := os.Stat(filepath.Join(dir, model.ProjectFile)); err == nil {
		t.Error("project.yaml was written into a folder that is not empty")
	}
}

// TestCreateGitOnly: a folder with only .git (after "git init") counts as empty.
func TestCreateGitOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(dir, Options{Name: "demo"}); err != nil {
		t.Fatal(err)
	}
}

func TestCreateForceKeepsFiles(t *testing.T) {
	dir := t.TempDir()
	own := "kind: Project\napiVersion: runnel/v1\nname: mine\n"
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(model.ProjectFile, own)
	write("triggers/nightly.yaml", "x")
	res, err := Create(dir, Options{Name: "demo", Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, model.ProjectFile)); string(got) != own {
		t.Errorf("project.yaml was overwritten: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, model.TriggersDir, gitKeep)); err == nil {
		t.Error(".gitkeep added to a folder with files")
	}
	if want := []string{model.ProjectFile, "triggers/"}; !slices.Equal(res.Skipped, want) {
		t.Errorf("skipped = %v, want %v", res.Skipped, want)
	}
	if slices.Contains(res.Created, model.ProjectFile) || !slices.Contains(res.Created, ExampleWorkflow) {
		t.Errorf("created = %v", res.Created)
	}

	// A second run adds nothing.
	res, err = Create(dir, Options{Name: "demo", Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Created) > 0 || len(res.Skipped) != len(Files("demo")) {
		t.Errorf("second run: created %v, skipped %v", res.Created, res.Skipped)
	}
}

func TestCreateNotDir(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(file, Options{Force: true}); !errors.Is(err, ErrNotDir) {
		t.Errorf("err = %v, want ErrNotDir", err)
	}
}

func TestCreateInvalidName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo")
	if _, err := Create(dir, Options{Name: "a/b"}); !errors.Is(err, ErrInvalidName) {
		t.Errorf("err = %v, want ErrInvalidName", err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("folder created for an invalid name")
	}
}

func TestCheckName(t *testing.T) {
	for _, name := range []string{"demo", "Demo_2", "orders.import", "2024", "Bestellungen-Müller", strings.Repeat("a", maxNameLength)} {
		if err := CheckName(name); err != nil {
			t.Errorf("CheckName(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"", "a b", "-x", ".x", "_x", "a/b", `a\b`, `a"b`, "a:b", "a\nb", strings.Repeat("a", maxNameLength+1)} {
		if err := CheckName(name); !errors.Is(err, ErrInvalidName) {
			t.Errorf("CheckName(%q) = %v, want ErrInvalidName", name, err)
		}
	}
}

func TestDefaultName(t *testing.T) {
	for _, tt := range []struct{ dir, want string }{
		{"demo", "demo"},
		{"My Project", "my-project"},
		{"  Orders (2024) ", "orders-2024"},
		{"Bestellungen Müller", "bestellungen-müller"},
		{"__.--", "project"},
		{"€€€", "project"},
		{strings.Repeat("ab ", 40), strings.Repeat("ab-", 22)[:maxNameLength]},
	} {
		got := DefaultName(filepath.Join(t.TempDir(), tt.dir))
		if got != tt.want {
			t.Errorf("DefaultName(%q) = %q, want %q", tt.dir, got, tt.want)
		}
		if err := CheckName(got); err != nil {
			t.Errorf("DefaultName(%q) = %q: %v", tt.dir, got, err)
		}
	}
}

// TestNamesReadBack: names that YAML would read as a number, a boolean or null are
// quoted, so the project keeps the name.
func TestNamesReadBack(t *testing.T) {
	for _, name := range []string{"demo", "true", "False", "null", "yes", "2024", "1e3", "0x1F", "1.0", "Müller"} {
		dir := filepath.Join(t.TempDir(), "p")
		if _, err := Create(dir, Options{Name: name}); err != nil {
			t.Fatal(err)
		}
		p, err := model.LoadProject(dir)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if p.Name != name {
			t.Errorf("name %q read back as %q", name, p.Name)
		}
	}
}
