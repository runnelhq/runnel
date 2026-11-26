package runs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/history"
)

// historyWriter writes runs to the history (ADR 0026) in a goroutine of its own, so that a
// slow or broken history file never holds up a run. Log lines are collected and written
// about every second; running runs write a sign of life every history.BeatEvery.
type historyWriter struct {
	store *history.Store
	warn  func(string)
	ops   chan recOp
	done  chan struct{}

	mu      sync.Mutex
	dropped bool   // the queue was full; reported once
	failing string // error of the last write; empty once a batch was written (GET /health)
}

// Size of the queue of the historyWriter; when it is full, entries are dropped (the run
// goes on, its history is incomplete) and the server log says so once.
const recQueue = 16384

type recKind int

const (
	recRun   recKind = iota // put the run (and, with nodes, all its actions)
	recNode                 // put one action or transform
	recLog                  // add a log line
	recEnd                  // the run has ended: write it, then drop it if del
	recClose                // stop the historyWriter
)

type recOp struct {
	kind  recKind
	run   Run
	hash  string
	nodes []*ActionState
	index int
	node  ActionState
	line  history.LogLine
	del   bool
}

func newHistoryWriter(store *history.Store, warn func(string)) *historyWriter {
	if warn == nil {
		warn = func(string) {}
	}
	r := &historyWriter{store: store, warn: warn, ops: make(chan recOp, recQueue), done: make(chan struct{})}
	go r.loop()
	return r
}

// send queues an operation without waiting; a full queue drops it.
func (r *historyWriter) send(op recOp) {
	select {
	case r.ops <- op:
	default:
		r.mu.Lock()
		first := !r.dropped
		r.dropped = true
		r.mu.Unlock()
		if first {
			r.warn("the run history cannot keep up; some runs are stored incomplete")
		}
	}
}

// close writes what is queued and stops the historyWriter.
func (r *historyWriter) close() {
	r.ops <- recOp{kind: recClose}
	<-r.done
}

// maxBatch is the most operations written in one transaction.
const maxBatch = 1000

// writerState is what the loop of the historyWriter keeps between transactions.
type writerState struct {
	logs    map[string][]history.LogLine // collected, not yet written
	running map[string]bool              // runs that write a sign of life
	known   map[string]bool              // runs written to the history, so their actions and log may follow
	waiting map[string]bool              // runs written at their end in this batch: log lines wait for it
	// lastWarn and suppressed limit the warnings about write errors to one a minute.
	lastWarn   time.Time
	suppressed int
}

func (r *historyWriter) loop() {
	defer close(r.done)
	ctx := context.Background()
	flush := time.NewTicker(time.Second)
	defer flush.Stop()
	beat := time.NewTicker(history.BeatEvery)
	defer beat.Stop()
	st := &writerState{logs: map[string][]history.LogLine{}, running: map[string]bool{}, known: map[string]bool{}, waiting: map[string]bool{}}
	for {
		select {
		case op := <-r.ops:
			// Write what is queued in one transaction: one by one, SQLite would not keep
			// up with many short runs (e.g. http triggers).
			batch := []recOp{op}
		collect:
			for len(batch) < maxBatch {
				select {
				case o := <-r.ops:
					batch = append(batch, o)
				default:
					break collect
				}
			}
			if r.write(ctx, st, batch) {
				return
			}
		case <-flush.C:
			r.report(st, r.store.Update(ctx, func(tx *history.Tx) error { return st.writeLogs(ctx, tx, "") }))
		case <-beat.C:
			ids := make([]string, 0, len(st.running))
			for id := range st.running {
				ids = append(ids, id)
			}
			r.report(st, r.store.Beat(ctx, ids))
		}
	}
}

// write applies a batch of operations in one transaction; if that fails, one by one, so
// that one bad entry does not lose the others. It reports whether the writer stops.
func (r *historyWriter) write(ctx context.Context, st *writerState, batch []recOp) (closing bool) {
	// A run that ends in this batch is written once at its end: its start and actions
	// before are left out (the end has them all), its log lines are kept.
	ended := map[string]bool{}
	for _, op := range batch {
		if op.kind == recEnd {
			ended[op.run.ID] = true
		}
	}
	if len(ended) > 0 {
		kept := batch[:0:0]
		for _, op := range batch {
			if ended[op.run.ID] && (op.kind == recRun || op.kind == recNode) {
				st.waiting[op.run.ID] = true // its log lines are written after its end
				continue
			}
			kept = append(kept, op)
		}
		batch = kept
	}
	err := r.store.Update(ctx, func(tx *history.Tx) error {
		for _, op := range batch {
			if err := r.apply(ctx, tx, st, op); err != nil {
				return err
			}
		}
		return nil
	})
	r.setError(err)
	if err != nil {
		r.report(st, err)
		for _, op := range batch {
			r.report(st, r.store.Update(ctx, func(tx *history.Tx) error { return r.apply(ctx, tx, st, op) }))
		}
	}
	for _, op := range batch {
		if op.kind == recClose {
			r.report(st, r.store.Update(ctx, func(tx *history.Tx) error { return st.writeLogs(ctx, tx, "") }))
			return true
		}
	}
	return false
}

// apply writes one operation in tx.
func (r *historyWriter) apply(ctx context.Context, tx *history.Tx, st *writerState, op recOp) error {
	id := op.run.ID
	switch op.kind {
	case recRun:
		if err := putRun(ctx, tx, op); err != nil {
			return err
		}
		st.running[id], st.known[id] = true, true
	case recNode:
		if st.known[id] { // else its run was dropped (full queue): drop the action too
			return putNode(ctx, tx, id, op.index, op.node)
		}
	case recLog:
		if st.known[id] || st.waiting[id] {
			st.logs[id] = append(st.logs[id], op.line)
			if st.known[id] && len(st.logs[id]) >= 200 {
				return st.writeLogs(ctx, tx, id)
			}
		}
	case recEnd:
		delete(st.running, id)
		delete(st.known, id)
		delete(st.waiting, id)
		if op.del {
			delete(st.logs, id)
			return tx.Delete(ctx, id)
		}
		if err := putRun(ctx, tx, op); err != nil {
			return err
		}
		return st.writeLogs(ctx, tx, id)
	}
	return nil
}

// writeLogs writes the collected log lines of a run, or of all runs for id "".
func (st *writerState) writeLogs(ctx context.Context, tx *history.Tx, id string) error {
	for rid, lines := range st.logs {
		if id != "" && rid != id {
			continue
		}
		if err := tx.AddLog(ctx, rid, lines); err != nil {
			return err
		}
		delete(st.logs, rid)
	}
	return nil
}

// report sends a write error to the server log, at most once a minute with the number
// of errors in between.
func (r *historyWriter) report(st *writerState, err error) {
	if err == nil {
		return
	}
	r.setError(err)
	now := time.Now()
	if !st.lastWarn.IsZero() && now.Sub(st.lastWarn) < time.Minute {
		st.suppressed++
		return
	}
	msg := fmt.Sprintf("cannot write the run history %s: %v", r.store.Path(), err)
	if st.suppressed > 0 {
		msg += fmt.Sprintf(" (and %d more errors since the last message)", st.suppressed)
	}
	st.lastWarn, st.suppressed = now, 0
	r.warn(msg)
}

// setError keeps the error of a write for lastError; nil clears it.
func (r *historyWriter) setError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failing = ""
	if err != nil {
		r.failing = err.Error()
	}
}

// lastError returns the error of the last write; empty if it worked.
func (r *historyWriter) lastError() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failing
}

func putRun(ctx context.Context, tx *history.Tx, op recOp) error {
	data, err := json.Marshal(op.run)
	if err != nil {
		return err
	}
	h := history.Run{ID: op.run.ID, Path: op.run.Path, Kind: op.run.Kind, Trigger: op.run.Trigger, Status: string(op.run.Status),
		Started: op.run.Started, Finished: op.run.Finished, Error: op.run.Error, FileHash: op.hash, Parent: op.run.Parent, Data: data}
	if len(data) > history.MaxDataBytes {
		// Outputs or parameters too large: keep the run without them, with a note.
		small := op.run
		small.Outputs, small.Params = nil, nil
		small.Error = joinError(small.Error, fmt.Sprintf("outputs and parameters not kept in the history: more than %d bytes", history.MaxDataBytes))
		if h.Data, err = json.Marshal(small); err != nil {
			return err
		}
	}
	if err := tx.PutRun(ctx, h); err != nil {
		return err
	}
	for i, n := range op.nodes {
		if err := putNode(ctx, tx, op.run.ID, i, *n); err != nil {
			return err
		}
	}
	return nil
}

func joinError(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

func putNode(ctx context.Context, tx *history.Tx, runID string, index int, a ActionState) error {
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return tx.PutNode(ctx, runID, history.Node{Index: index, ID: a.ID, Data: data})
}

// fileHash identifies the content of a workflow or pipeline file.
func fileHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}

// snapshot copies the state of a run for the historyWriter. m.mu must be held.
func snapshot(e *entry) Run {
	r := e.run
	r.LinkRows = slices.Clone(r.LinkRows)
	return r
}

// copyActions copies the states of the actions of a run. m.mu must be held.
func copyActions(e *entry) []*ActionState {
	out := make([]*ActionState, len(e.actions))
	for i, a := range e.actions {
		c := *a
		out[i] = &c
	}
	return out
}

// fromHistory turns a stored run into the run the API shows. The columns win over the
// JSON: a run of a stopped process was set to failed in them only.
func fromHistory(h history.Run) (Run, error) {
	var r Run
	if err := json.Unmarshal(h.Data, &r); err != nil {
		return Run{}, fmt.Errorf("run %s in the history: %w", h.ID, err)
	}
	r.ID, r.Status, r.Error = h.ID, workflow.Status(h.Status), h.Error
	if h.Finished != nil {
		f := *h.Finished
		r.Finished = &f
		if r.DurationMs == 0 {
			r.DurationMs = f.Sub(h.Started).Milliseconds()
		}
	}
	return r, nil
}

// detailFromHistory turns a stored run with its actions and log into a Detail.
func detailFromHistory(d history.Detail) (Detail, error) {
	run, err := fromHistory(d.Run)
	if err != nil {
		return Detail{}, err
	}
	out := Detail{Run: run, Log: []LogLine{}, LogTruncated: d.LogTruncated}
	for _, n := range d.Nodes {
		var a ActionState
		if err := json.Unmarshal(n.Data, &a); err != nil {
			return Detail{}, fmt.Errorf("run %s in the history: %w", run.ID, err)
		}
		if run.Status != StatusRunning && (a.Status == StatusRunning || a.Status == workflow.Waiting) {
			a.Status = run.Status // the process stopped while the action ran
		}
		out.Actions = append(out.Actions, &a)
	}
	for _, l := range d.Log {
		out.Log = append(out.Log, LogLine{Time: l.Time, Level: l.Level, ActionID: l.ActionID, Message: l.Message})
	}
	return out, nil
}

// mergeRuns adds the stored runs to those in memory (newest first), without the ones
// that are in memory, newest first, at most limit.
func mergeRuns(mem []Run, stored []Run, limit int) []Run {
	seen := make(map[string]bool, len(mem))
	for _, r := range mem {
		seen[r.ID] = true
	}
	out := slices.Clone(mem)
	for _, r := range stored {
		if !seen[r.ID] {
			out = append(out, r)
		}
	}
	// Run ids are ULIDs: sorting them sorts by start.
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
