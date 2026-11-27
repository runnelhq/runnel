// Package scaffold creates a new project: project.yaml, the folders of a project, an
// environment dev, a small example workflow and .gitignore (roadmap R9, "runnel init";
// R10 creates a project from the editor the same way).
package scaffold

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/runnelhq/runnel/internal/model"
)

// Folder and file names of a new project.
const (
	WorkflowsDir    = "workflows"
	PipelinesDir    = "pipelines"
	ExampleWorkflow = WorkflowsDir + "/hello.workflow.yaml"
	Environment     = "dev"
	gitKeep         = ".gitkeep"
	maxNameLength   = 64
)

// ErrNotEmpty is returned for a folder that already has content, unless Options.Force is set.
var ErrNotEmpty = errors.New("folder is not empty")

// ErrInvalidName is returned for a project name that Create does not accept.
var ErrInvalidName = errors.New("invalid project name")

// ErrNotDir is returned when the target exists but is not a folder.
var ErrNotDir = errors.New("not a folder")

var (
	// namePattern: letters and digits, also -, _ and . inside; no spaces, quotes or path separators.
	namePattern = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}._-]*$`)
	// plainPattern: names that YAML reads as a string without quotes, except words like null or true.
	plainPattern = regexp.MustCompile(`^\p{L}[\p{L}\p{N}_-]*$`)
)

// Options control Create.
type Options struct {
	// Name of the project; empty: DefaultName of the folder.
	Name string
	// Force writes into a folder that is not empty; existing files are kept.
	Force bool
}

// Result lists what Create did; paths are relative to the project folder, with "/"
// and a trailing "/" for folders.
type Result struct {
	Name    string
	Created []string
	// Skipped are the files that already existed and were kept (only with Force).
	Skipped []string
}

// File is a file or folder of a new project.
type File struct {
	// Path relative to the project folder with "/"; folders end with "/".
	Path    string
	Content string
}

// CheckName returns an error wrapping ErrInvalidName if name cannot be the name of a
// new project.
func CheckName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("%w: the name must not be empty", ErrInvalidName)
	case len(name) > maxNameLength:
		return fmt.Errorf("%w %q: at most %d characters", ErrInvalidName, name, maxNameLength)
	case !namePattern.MatchString(name):
		return fmt.Errorf("%w %q: use letters, digits, -, _ and ., starting with a letter or digit", ErrInvalidName, name)
	}
	return nil
}

// DefaultName derives a project name from the folder dir: its base name in lower case,
// other characters than letters, digits, -, _ and . replaced by "-"; "project" if
// nothing is left.
func DefaultName(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	var b strings.Builder
	for _, r := range strings.ToLower(filepath.Base(dir)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._-", r) {
			b.WriteRune(r)
		} else if s := b.String(); s != "" && !strings.HasSuffix(s, "-") {
			b.WriteByte('-')
		}
	}
	name := strings.TrimRight(strings.TrimLeft(b.String(), "._-"), "._-")
	for len(name) > maxNameLength {
		_, size := utf8.DecodeLastRuneInString(name)
		name = strings.TrimRight(name[:len(name)-size], "._-")
	}
	if CheckName(name) != nil {
		return "project"
	}
	return name
}

// Files returns the files and folders of a new project with the given name, in the
// order Create writes them.
func Files(name string) []File {
	return []File{
		{Path: model.ProjectFile, Content: projectYAML(name)},
		{Path: model.EnvironmentsDir + "/" + Environment + ".yaml", Content: environmentYAML},
		{Path: ExampleWorkflow, Content: workflowYAML},
		{Path: PipelinesDir + "/"},
		{Path: model.TriggersDir + "/"},
		{Path: model.ConnectionsDir + "/"},
		{Path: ".gitignore", Content: gitignore},
	}
}

// Create creates a new project in dir; the folder is created if missing. A folder
// with content (other than .git) is refused with ErrNotEmpty unless opts.Force is
// set; existing files are never overwritten. Empty folders get a .gitkeep file so
// that Git keeps them.
func Create(dir string, opts Options) (*Result, error) {
	name := opts.Name
	if name == "" {
		name = DefaultName(dir)
	}
	if err := CheckName(name); err != nil {
		return nil, err
	}
	empty, err := IsEmpty(dir)
	if err != nil {
		return nil, err
	}
	if !empty && !opts.Force {
		return nil, ErrNotEmpty
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	res := &Result{Name: name}
	for _, f := range Files(name) {
		target := filepath.Join(dir, filepath.FromSlash(f.Path))
		if strings.HasSuffix(f.Path, "/") {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return res, err
			}
			if entries, err := os.ReadDir(target); err != nil {
				return res, err
			} else if len(entries) > 0 {
				res.Skipped = append(res.Skipped, f.Path)
				continue
			}
			target = filepath.Join(target, gitKeep)
		} else if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return res, err
		}
		created, err := writeNew(target, f.Content)
		if err != nil {
			return res, err
		}
		if created {
			res.Created = append(res.Created, f.Path)
		} else {
			res.Skipped = append(res.Skipped, f.Path)
		}
	}
	return res, nil
}

// IsEmpty reports whether dir is missing or has no entries other than .git: whether
// Create accepts it without Options.Force.
func IsEmpty(dir string) (bool, error) {
	info, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%s: %w", dir, ErrNotDir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Name() != ".git" {
			return false, nil
		}
	}
	return true, nil
}

// writeNew writes content to a new file; false if the file already exists.
func writeNew(path, content string) (bool, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = f.WriteString(content)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err == nil, err
}

// yamlScalar is name as a YAML value: quoted if it would read as a number, a boolean or null.
func yamlScalar(name string) string {
	if plainPattern.MatchString(name) {
		switch strings.ToLower(name) {
		case "null", "true", "false", "yes", "no", "on", "off", "y", "n":
		default:
			return name
		}
	}
	return `"` + name + `"`
}

func projectYAML(name string) string {
	return "# The project: settings and variables for all workflows and pipelines in this folder.\n" +
		"# Check it with:  runnel validate\n" +
		"kind: " + model.KindProject + "\n" +
		"apiVersion: " + model.APIVersion + "\n" +
		"name: " + yamlScalar(name) + "\n" +
		"defaultEnvironment: " + Environment + "\n" +
		"variables:\n" +
		"  TARGET: World\n"
}

const environmentYAML = "# Variables for local development; add environments/prod.yaml for the production system.\n" +
	"kind: " + model.KindEnvironment + "\n" +
	"apiVersion: " + model.APIVersion + "\n" +
	"name: " + Environment + "\n" +
	"description: Local development\n" +
	"variables:\n" +
	"  STAGE: development\n"

const workflowYAML = "# A small example workflow. Run it with:  runnel run " + ExampleWorkflow + "\n" +
	"kind: " + model.KindWorkflow + "\n" +
	"apiVersion: " + model.APIVersion + "\n" +
	"name: hello\n" +
	"description: Logs a greeting and ends successfully.\n" +
	"parameters:\n" +
	"  - name: GREETING\n" +
	"    description: Word to greet with\n" +
	"    default: Hello\n" +
	"actions:\n" +
	"  - id: start\n" +
	"    type: core.start\n" +
	"  - id: greet\n" +
	"    type: core.log\n" +
	"    with:\n" +
	"      message: \"${GREETING}, ${TARGET}! (${STAGE})\"\n" +
	"  - id: done\n" +
	"    type: core.success\n" +
	"links:\n" +
	"  - { from: start, to: greet }\n" +
	"  - { from: greet, to: done }\n" +
	"layout:\n" +
	"  start: { x: 40, y: 120 }\n" +
	"  greet: { x: 280, y: 120 }\n" +
	"  done: { x: 520, y: 120 }\n"

const gitignore = "# Runnel keeps run history, logs, certificates and plugin approvals in its data folder,\n" +
	"# not in the project. Secrets belong in ${env:NAME} or ${secret:NAME}, not in these files.\n" +
	"\n" +
	"# Output of runs: relative paths write into the project folder\n" +
	"/out/\n" +
	"\n" +
	"# Windows and Office files\n" +
	"Thumbs.db\n" +
	"desktop.ini\n" +
	"~$*\n"
