package triggers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/model"
)

// watch is the state of a file trigger (roadmap S38). The folder is polled; no file
// system notifications are used, so that network drives and UNC paths work the same.
//
// A file is new if it was not there before or has changed (size or modification time)
// since its run was started. It is ready once it is unchanged over two checks, so that
// files still being written are not processed half. A file counts as processed only
// when its run was started: if the start is skipped (overlap: skip) or rejected (e.g.
// settings.maxConcurrentRuns), the file is tried again at the next check.
type watch struct {
	folder  string // absolute
	primed  bool   // the folder was read at least once
	seen    map[string]fileState
	pending map[string]fileState // state at the last check of files not yet started
	problem string               // last problem reading the folder (logged once)
}

type fileState struct {
	size int64
	mod  int64
}

func newWatch(t *model.Trigger, projectDir string) *watch {
	folder := t.Folder(projectDir)
	if abs, err := filepath.Abs(folder); err == nil {
		folder = abs
	}
	return &watch{folder: folder, seen: map[string]fileState{}, pending: map[string]fileState{}}
}

// sameWatch reports whether a changed trigger file still watches the same files, so
// that the files already processed are kept.
func sameWatch(a, b *model.Trigger) bool {
	return a.Type == model.TriggerFile && b.Type == model.TriggerFile && a.With.Path == b.With.Path &&
		strings.EqualFold(a.FilePattern(), b.FilePattern()) && a.With.Existing == b.With.Existing
}

// scan returns the files in folder whose names match pattern (case is ignored).
func scan(folder, pattern string) (map[string]fileState, error) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, err
	}
	pattern = strings.ToLower(pattern)
	out := map[string]fileState{}
	for _, de := range entries {
		if !de.Type().IsRegular() {
			continue
		}
		if ok, _ := filepath.Match(pattern, strings.ToLower(de.Name())); !ok {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue // removed in the meantime
		}
		out[filepath.Join(folder, de.Name())] = fileState{info.Size(), info.ModTime().UnixNano()}
	}
	return out, nil
}

// checkFolder looks into the folder of a file trigger and starts runs for the files
// that are ready.
func (s *Scheduler) checkFolder(e *entry, now time.Time) {
	t := e.t
	s.mu.Lock()
	w := e.files
	s.mu.Unlock()

	cur, err := scan(w.folder, t.FilePattern())
	s.mu.Lock()
	if err != nil {
		msg := fmt.Sprintf("cannot read folder %s: %v", w.folder, err)
		if errors.Is(err, fs.ErrNotExist) {
			msg = fmt.Sprintf("folder %s not found", w.folder)
		}
		if msg != w.problem {
			w.problem = msg
			e.last = &Fired{Time: now, Error: msg}
			s.mu.Unlock()
			s.opts.Warn(fmt.Sprintf("trigger %q: %s; trying again every %s", t.Name, msg, t.PollInterval()))
			return
		}
		s.mu.Unlock()
		return
	}
	if w.problem != "" {
		w.problem = ""
		s.opts.Log(fmt.Sprintf("trigger %q: folder %s can be read again", t.Name, w.folder))
	}
	if !w.primed {
		w.primed = true
		if t.With.Existing == model.ExistingIgnore {
			w.seen = cur
			s.mu.Unlock()
			return
		}
	}
	for p := range w.seen {
		if _, ok := cur[p]; !ok {
			delete(w.seen, p) // a file created again under the same name counts as new
		}
	}
	var ready []string
	pending := map[string]fileState{}
	for p, st := range cur {
		if old, ok := w.seen[p]; ok && old == st {
			continue
		}
		if old, ok := w.pending[p]; ok && old == st {
			ready = append(ready, p)
		}
		pending[p] = st
	}
	w.pending = pending
	s.mu.Unlock()

	slices.Sort(ready)
	if len(ready) == 0 {
		return
	}
	done := func(files ...string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, p := range files {
			w.seen[p] = cur[p]
		}
	}
	if t.FileMode() == model.FileBatch {
		list, _ := json.Marshal(ready)
		if s.start(e, now, map[string]string{t.With.Param: string(list)}, ready) {
			done(ready...)
		}
		return
	}
	for _, p := range ready {
		if !s.start(e, now, map[string]string{t.With.Param: p}, []string{p}) {
			return // the others are tried again at the next check
		}
		done(p)
		if t.OverlapMode() == model.OverlapSkip {
			return // one run at a time: the next file at the next check
		}
	}
}
