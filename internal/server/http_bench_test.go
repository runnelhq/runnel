package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/transforms"
)

// BenchmarkHTTPTrigger measures requests to an http trigger with response: run (load
// probe of S38b2): routing, parameters, preparing the run (cached) and the answer.
//
//	go test ./internal/server -run XXX -bench HTTPTrigger -benchmem
func BenchmarkHTTPTrigger(b *testing.B) {
	dir := b.TempDir()
	for name, content := range map[string]string{
		"project.yaml": "kind: Project\napiVersion: runnel/v1\nname: p\n",
		"workflows/w.workflow.yaml": "kind: Workflow\napiVersion: runnel/v1\nname: w\nparameters:\n  - { name: ID, type: integer }\n" +
			"actions:\n  - { id: start, type: core.start }\n  - { id: reply, type: http.respond, with: { body: { id: \"{{ params.ID }}\" } } }\n" +
			"  - { id: log, type: core.log, with: { message: \"answered {{ params.ID }}\" } }\n" +
			"links:\n  - { from: start, to: reply }\n  - { from: reply, to: log }\n",
		"triggers/w.yaml": "kind: Trigger\napiVersion: runnel/v1\nname: w\ntype: http\ntarget: workflows/w.workflow.yaml\n" +
			"with: { method: GET, path: \"/w/{ID}\", auth: none, response: run, history: none }\n",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	s, err := New(Options{Dir: dir, Registry: actions.Builtin(), Transforms: transforms.Builtin()})
	if err != nil {
		b.Fatal(err)
	}
	s.triggers.Reload()
	if p := s.triggers.Problems(); len(p) > 0 {
		b.Fatal(p)
	}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			r := httptest.NewRequest(http.MethodGet, "/w/7", strings.NewReader(""))
			r.Host = "127.0.0.1:7301"
			w := httptest.NewRecorder()
			s.triggers.ServeHTTP(w, r)
			if w.Code != 200 {
				b.Error(w.Code, w.Body)
				return
			}
		}
	})
}
