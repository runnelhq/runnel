package runs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/engine/pipeline"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/transforms"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const copyPipeline = `kind: Pipeline
apiVersion: runnel/v1
name: copy
transforms:
  - { id: read, type: csv.input, with: { path: data/in.csv, delimiter: ";" } }
  - { id: write, type: csv.output, with: { path: "OUT" } }
links:
  - { from: read, to: write }
`

// TestRelativePaths: relative paths of transforms refer to the project folder, without a
// project to the folder of the pipeline file, never to the working directory (ADR 0020).
func TestRelativePaths(t *testing.T) {
	t.Chdir(t.TempDir()) // the working directory must not matter
	project := t.TempDir()
	writeTestFile(t, filepath.Join(project, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: p\n")
	writeTestFile(t, filepath.Join(project, "data", "in.csv"), "a\n1\n")
	for _, tt := range []struct {
		name, out, want string
	}{
		{"relative", "out/copy.csv", filepath.Join(project, "out", "copy.csv")},
		{"built-in variable", "${PROJECT_DIR}/out/var.csv", filepath.Join(project, "out", "var.csv")},
	} {
		if err := os.MkdirAll(filepath.Join(project, "out"), 0o755); err != nil {
			t.Fatal(err)
		}
		p, err := PreparePipeline(PipelineSpec{Name: "pipelines/copy.pipeline.yaml", ProjectDir: project, Registry: transforms.Builtin(),
			Data: []byte(strings.Replace(copyPipeline, "OUT", tt.out, 1))})
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if res := p.Runner.Run(context.Background()); res.Status != pipeline.Succeeded {
			t.Fatalf("%s: %v", tt.name, res.Err)
		}
		if _, err := os.Stat(tt.want); err != nil {
			t.Errorf("%s: %v", tt.name, err)
		}
	}

	// Without a project: the folder of the pipeline file.
	loose := t.TempDir()
	writeTestFile(t, filepath.Join(loose, "data", "in.csv"), "a\n1\n")
	if err := os.MkdirAll(filepath.Join(loose, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(loose, "copy.pipeline.yaml")
	p, err := PreparePipeline(PipelineSpec{Name: "copy.pipeline.yaml", File: file, Registry: transforms.Builtin(),
		Data: []byte(strings.Replace(copyPipeline, "OUT", "out/loose.csv", 1))})
	if err != nil {
		t.Fatal(err)
	}
	if res := p.Runner.Run(context.Background()); res.Status != pipeline.Succeeded {
		t.Fatal(res.Err)
	}
	if _, err := os.Stat(filepath.Join(loose, "out", "loose.csv")); err != nil {
		t.Error(err)
	}
}

// TestRelativePathsOfActions: file actions resolve against the project folder; the
// folder created by file.mkdir is returned as an absolute path for later actions.
func TestRelativePathsOfActions(t *testing.T) {
	t.Chdir(t.TempDir())
	project := t.TempDir()
	writeTestFile(t, filepath.Join(project, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: p\n")
	writeTestFile(t, filepath.Join(project, "inbox", "a.csv"), "x\n")
	wf := `kind: Workflow
apiVersion: runnel/v1
name: files
outputs:
  - { name: FOLDER, value: "{{ results.folder.output }}" }
actions:
  - { id: start, type: core.start }
  - { id: folder, type: file.mkdir, with: { path: out/archive } }
  - { id: copy, type: file.copy, with: { source: "inbox/*.csv", target: "{{ results.folder.output }}" } }
links:
  - { from: start, to: folder }
  - { from: folder, to: copy }
`
	p, err := Prepare(Spec{Name: "workflows/files.workflow.yaml", File: filepath.Join(project, "workflows", "files.workflow.yaml"),
		ProjectDir: project, Data: []byte(wf), Registry: actions.Builtin()})
	if err != nil {
		t.Fatal(err)
	}
	res := p.Runner.Run(context.Background())
	if res.Status != workflow.Succeeded {
		t.Fatal(res.Err)
	}
	if out := res.Outputs["FOLDER"]; out != filepath.Join(project, "out", "archive") {
		t.Errorf("output of file.mkdir: %v", out)
	}
	if _, err := os.Stat(filepath.Join(project, "out", "archive", "a.csv")); err != nil {
		t.Error(err)
	}
}
