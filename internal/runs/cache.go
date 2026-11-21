package runs

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/runnelhq/runnel/internal/model"
)

// The cache of prepared files (roadmap S38b2): the same workflows and pipelines are
// started again and again, e.g. for each request to an http trigger. Parsing and
// checking them is a pure function of their content, so the results are kept by the
// hash of the content; a changed file has another hash and is checked anew. The
// project is kept by the state (size, time) of its files. Only results without errors
// are kept; the cached values are shared and must not be changed.

// maxCached is the number of entries kept per kind; the cache is cleared when full.
const maxCached = 256

type cache[T any] struct {
	mu sync.Mutex
	m  map[string]T
}

func (c *cache[T]) get(key string, load func() (T, error)) (T, error) {
	c.mu.Lock()
	v, ok := c.m[key]
	c.mu.Unlock()
	if ok {
		return v, nil
	}
	v, err := load()
	if err != nil {
		return v, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil || len(c.m) >= maxCached {
		c.m = map[string]T{}
	}
	c.m[key] = v
	return v, nil
}

type checkedWorkflow struct {
	w     *model.Workflow
	diags model.Errors // warnings only
}

type checkedPipeline struct {
	p     *model.Pipeline
	diags model.Errors
}

var (
	workflowCache cache[checkedWorkflow]
	pipelineCache cache[checkedPipeline]
	projectCache  cache[*model.Project]
)

func contentKey(name string, data []byte) string {
	return fmt.Sprintf("%s\x00%x", name, sha256.Sum256(data))
}

// errorsOnly marks results that are not cached: files with errors.
type errorsOnly struct{ diags model.Errors }

func (e errorsOnly) Error() string { return e.diags.Error() }

// parseWorkflow parses and validates a workflow file. err is the error of parsing, or
// the problems found (with warnings) if there are errors; warnings without errors are
// returned with the workflow.
func parseWorkflow(name string, data []byte) (*model.Workflow, model.Errors, error) {
	c, err := workflowCache.get(contentKey(name, data), func() (checkedWorkflow, error) {
		w, err := model.ParseWorkflow(name, data)
		if err != nil {
			return checkedWorkflow{w: w}, err
		}
		diags := w.Validate()
		if diags.HasErrors() {
			return checkedWorkflow{w: w}, errorsOnly{diags}
		}
		return checkedWorkflow{w: w, diags: diags}, nil
	})
	var e errorsOnly
	if errors.As(err, &e) {
		return c.w, e.diags, nil
	}
	return c.w, slices.Clone(c.diags), err
}

// parsePipeline is parseWorkflow for pipelines.
func parsePipeline(name string, data []byte) (*model.Pipeline, model.Errors, error) {
	c, err := pipelineCache.get(contentKey(name, data), func() (checkedPipeline, error) {
		p, err := model.ParsePipeline(name, data)
		if err != nil {
			return checkedPipeline{p: p}, err
		}
		diags := p.Validate()
		if diags.HasErrors() {
			return checkedPipeline{p: p}, errorsOnly{diags}
		}
		return checkedPipeline{p: p, diags: diags}, nil
	})
	var e errorsOnly
	if errors.As(err, &e) {
		return c.p, e.diags, nil
	}
	return c.p, slices.Clone(c.diags), err
}

// loadProject loads the project, kept while its files (project.yaml, environments)
// are unchanged.
func loadProject(dir string) (*model.Project, error) {
	return projectCache.get(projectStamp(dir), func() (*model.Project, error) { return model.LoadProject(dir) })
}

// projectStamp describes the state of the files of a project.
func projectStamp(dir string) string {
	var b strings.Builder
	b.WriteString(dir)
	files, _ := filepath.Glob(filepath.Join(dir, model.EnvironmentsDir, "*.yaml"))
	for _, f := range append([]string{filepath.Join(dir, model.ProjectFile)}, files...) {
		if info, err := os.Stat(f); err == nil {
			fmt.Fprintf(&b, "\x00%s %d %d", f, info.Size(), info.ModTime().UnixNano())
		}
	}
	return b.String()
}
