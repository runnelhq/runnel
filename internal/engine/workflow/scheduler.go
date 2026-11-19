package workflow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/model"
)

// linkState is the state of a non-loop link during a run (ADR 0003).
type linkState int

const (
	pending linkState = iota
	fired
	dead
)

// completion is sent by an action's goroutine when it has finished.
type completion struct {
	actionID string
	res      action.Result
	err      error
	start    time.Time
}

// scheduler runs one workflow run. All state is owned by the goroutine calling run;
// actions execute in their own goroutines and report back via the done channel.
type scheduler struct {
	r      *Runner
	ctx    context.Context
	cancel context.CancelFunc
	runID  string

	links     []linkState     // by link index; only non-loop links are used
	activated map[string]bool // for join all/first: action started (or skipped) in the current iteration
	runs      map[string]int  // executions per action
	running   int
	done      chan completion

	// Join timeouts (ADR 0003): one timer per waiting action; the generation
	// distinguishes timers of earlier loop iterations.
	joinTimers map[string]*time.Timer
	joinGen    map[string]int
	timeouts   chan joinTimeout

	unhandled []error // failures not followed by any link
	ended     bool    // core.success/core.abort, run limit or cancellation ended the run
	endStatus Status
	endErr    error
}

func newScheduler(ctx context.Context, r *Runner, runID string) *scheduler {
	ctx, cancel := context.WithCancel(ctx)
	return &scheduler{
		r:          r,
		ctx:        ctx,
		cancel:     cancel,
		runID:      runID,
		links:      make([]linkState, len(r.w.Links)),
		activated:  map[string]bool{},
		runs:       map[string]int{},
		done:       make(chan completion),
		joinTimers: map[string]*time.Timer{},
		joinGen:    map[string]int{},
		timeouts:   make(chan joinTimeout),
	}
}

type joinTimeout struct {
	actionID string
	gen      int
	after    time.Duration
}

// run executes the workflow until no action is running and none can start any more.
func (s *scheduler) run() (Status, error) {
	defer s.cancel()
	defer func() {
		for _, t := range s.joinTimers {
			t.Stop()
		}
	}()
	s.activate(s.r.start)
	for s.running > 0 {
		select {
		case c := <-s.done:
			s.running--
			s.finished(c)
		case jt := <-s.timeouts:
			s.joinTimedOut(jt)
		}
	}
	switch {
	case s.ended:
		return s.endStatus, s.endErr
	case len(s.unhandled) == 1:
		return Failed, s.unhandled[0]
	case len(s.unhandled) > 1:
		return Failed, errors.Join(s.unhandled...)
	}
	return Succeeded, nil
}

// end stops the run: no further actions start, running ones are cancelled.
func (s *scheduler) end(status Status, err error) {
	if s.ended {
		return
	}
	s.ended, s.endStatus, s.endErr = true, status, err
	s.cancel()
}

// activate starts an action in its own goroutine.
func (s *scheduler) activate(id string) {
	if s.ended {
		return
	}
	if err := s.ctx.Err(); err != nil {
		s.end(Failed, err)
		return
	}
	s.runs[id]++
	if s.runs[id] > s.r.opts.MaxActionRuns {
		s.end(Failed, fmt.Errorf("action %q ran more than %d times; stopping (endless loop?)", id, s.r.opts.MaxActionRuns))
		return
	}
	a := s.r.actions[id]
	start := s.r.opts.Now()
	s.r.emit(Event{Time: start, Kind: ActionStarted, RunID: s.runID, ActionID: a.ID, ActionType: a.Type})
	s.running++
	go func() {
		res, err := s.r.executeAction(s.ctx, s.runID, a)
		s.done <- completion{actionID: id, res: res, err: err, start: start}
	}()
}

// finished processes the result of an action and decides which links fire.
func (s *scheduler) finished(c completion) {
	a := s.r.actions[c.actionID]
	status := Succeeded
	switch {
	case c.err != nil && s.ctx.Err() != nil && errors.Is(c.err, s.ctx.Err()):
		status = Cancelled // stopped because the run ended, timed out or was cancelled
	case c.err != nil || c.res.End == action.EndFailure:
		status = Failed
	}
	end := s.r.opts.Now()
	s.r.emit(Event{Time: end, Kind: ActionFinished, RunID: s.runID, ActionID: a.ID, ActionType: a.Type,
		Status: status, Err: c.err, Message: c.res.Message, Duration: end.Sub(c.start)})

	if s.ended {
		return // run already decided; results of cancelled branches are ignored
	}
	switch {
	case c.err == nil && c.res.End == action.EndSuccess:
		s.end(Succeeded, nil)
		return
	case c.err == nil && c.res.End == action.EndFailure:
		s.end(Failed, errors.New(c.res.Message))
		return
	}

	ok := c.err == nil
	if !ok && a.ContinueOnError {
		s.r.log(s.runID, a, slog.LevelWarn, fmt.Sprintf("error ignored (continueOnError): %v", c.err))
		ok = true
	}

	anyFired := false
	for _, li := range s.r.out[a.ID] {
		if follows(s.r.w.Links[li], ok) {
			anyFired = true
			s.fire(li)
		} else {
			s.kill(li)
		}
	}
	if !ok && !anyFired {
		s.unhandled = append(s.unhandled, fmt.Errorf("action %q failed: %w", a.ID, c.err))
	}
}

// follows reports whether a link is taken after an action with the given outcome.
func follows(l model.Link, ok bool) bool {
	switch kind, _ := l.Condition(); kind {
	case model.OnSuccess:
		return ok
	case model.OnFailure:
		return !ok
	case model.Always:
		return true
	}
	return false
}

// fire marks a link as taken. A loop link starts its target again (a new iteration);
// other links count towards the join of their target.
func (s *scheduler) fire(li int) {
	l := s.r.w.Links[li]
	if l.Loop {
		s.resetIteration(l.To)
		s.activated[l.To] = true
		s.activate(l.To)
		return
	}
	s.links[li] = fired
	s.evaluate(l.To, true)
}

// kill marks a link as not taken (dead). Loop links are not part of joins.
func (s *scheduler) kill(li int) {
	l := s.r.w.Links[li]
	if l.Loop {
		return
	}
	s.links[li] = dead
	s.evaluate(l.To, false)
}

// evaluate decides, after an incoming link of id changed, whether id starts, waits or is skipped.
func (s *scheduler) evaluate(id string, justFired bool) {
	if s.ended {
		return
	}
	var nPending, nFired int
	for _, li := range s.r.in[id] {
		switch s.links[li] {
		case pending:
			nPending++
		case fired:
			nFired++
		}
	}
	switch s.joinMode(id) {
	case model.JoinEach:
		switch {
		case justFired:
			s.activated[id] = true
			s.activate(id)
		case nPending == 0 && nFired == 0 && !s.activated[id]:
			s.skip(id)
		}
	case model.JoinFirst:
		switch {
		case s.activated[id]:
		case justFired:
			s.activated[id] = true
			s.activate(id)
		case nPending == 0 && nFired == 0:
			s.skip(id)
		}
	default: // all
		switch {
		case s.activated[id]:
		case nPending > 0:
			if nFired > 0 {
				if justFired {
					a := s.r.actions[id]
					links := "links"
					if nPending == 1 {
						links = "link"
					}
					msg := fmt.Sprintf("waiting for %d more incoming %s (join: all)", nPending, links)
					s.r.emit(Event{Time: s.r.opts.Now(), Kind: ActionWaiting, RunID: s.runID, ActionID: id, ActionType: a.Type,
						Status: Waiting, Message: msg})
				}
				s.startJoinTimer(id)
			}
		case nFired > 0:
			s.activated[id] = true
			s.activate(id)
		default:
			s.skip(id)
		}
	}
}

func (s *scheduler) joinMode(id string) model.JoinMode {
	if j := s.r.actions[id].Join; j != "" {
		return j
	}
	if s.r.opts.DefaultJoin != "" {
		return s.r.opts.DefaultJoin
	}
	return model.JoinAll
}

// skip marks an action as not run and kills its outgoing links (dead-path elimination).
func (s *scheduler) skip(id string) {
	s.activated[id] = true
	a := s.r.actions[id]
	s.r.emit(Event{Time: s.r.opts.Now(), Kind: ActionSkipped, RunID: s.runID, ActionID: a.ID, ActionType: a.Type, Status: Skipped})
	for _, li := range s.r.out[id] {
		s.kill(li)
	}
}

// resetIteration prepares a new loop iteration starting at id: the link states and
// activation flags of everything downstream of id are reset (ADR 0003).
func (s *scheduler) resetIteration(id string) {
	region := map[string]bool{id: true}
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, li := range s.r.out[cur] {
			l := s.r.w.Links[li]
			if l.Loop {
				continue
			}
			s.links[li] = pending
			if !region[l.To] {
				region[l.To] = true
				queue = append(queue, l.To)
			}
		}
	}
	for a := range region {
		delete(s.activated, a)
		s.stopJoinTimer(a)
	}
}

// startJoinTimer starts the joinTimeout of an action once its first incoming link has fired.
func (s *scheduler) startJoinTimer(id string) {
	d, _ := s.r.actions[id].JoinTimeout.Value()
	if d <= 0 || s.joinTimers[id] != nil {
		return
	}
	jt := joinTimeout{actionID: id, gen: s.joinGen[id], after: d}
	s.joinTimers[id] = time.AfterFunc(d, func() {
		select {
		case s.timeouts <- jt:
		case <-s.ctx.Done():
		}
	})
}

func (s *scheduler) stopJoinTimer(id string) {
	if t := s.joinTimers[id]; t != nil {
		t.Stop()
		delete(s.joinTimers, id)
	}
	s.joinGen[id]++
}

// joinTimedOut fails an action whose join waited longer than its joinTimeout.
// The action does not run; its failure links are followed.
func (s *scheduler) joinTimedOut(jt joinTimeout) {
	id := jt.actionID
	if s.ended || s.activated[id] || jt.gen != s.joinGen[id] {
		return
	}
	s.activated[id] = true
	delete(s.joinTimers, id)
	var waiting []string
	for _, li := range s.r.in[id] {
		if s.links[li] == pending {
			waiting = append(waiting, s.r.w.Links[li].From)
		}
	}
	s.finished(completion{actionID: id, start: s.r.opts.Now(),
		err: fmt.Errorf("join timeout: still waiting for %v after %s", waiting, jt.after)})
}
