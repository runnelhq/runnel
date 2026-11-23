package runs

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/engine/pipeline"
	"github.com/runnelhq/runnel/internal/engine/workflow"
)

func TestDescribe(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		e    workflow.Event
		want Line
	}{
		{workflow.Event{Kind: workflow.RunStarted, RunID: "R1"}, Line{Level: slog.LevelInfo, Msg: `workflow "w" started (environment test), run R1`}},
		{workflow.Event{Kind: workflow.ActionStarted, ActionID: "a", ActionType: "core.log"}, Line{Level: slog.LevelDebug, Step: "a", Msg: "started (core.log)", Detail: true}},
		{workflow.Event{Kind: workflow.Log, ActionID: "a", Level: slog.LevelInfo, Message: "hi"}, Line{Level: slog.LevelInfo, Step: "a", Msg: "hi"}},
		{workflow.Event{Kind: workflow.Log, ActionID: "a", Level: slog.LevelDebug, Message: "hi"}, Line{Level: slog.LevelDebug, Step: "a", Msg: "hi", Detail: true}},
		{workflow.Event{Kind: workflow.ActionSkipped, ActionID: "a"}, Line{Level: slog.LevelDebug, Step: "a", Msg: "skipped (no incoming link applies)", Detail: true}},
		{workflow.Event{Kind: workflow.ActionFinished, ActionID: "a", Status: workflow.Succeeded, Duration: 1500 * time.Microsecond},
			Line{Level: slog.LevelDebug, Step: "a", Msg: "succeeded in 2ms", Detail: true}},
		{workflow.Event{Kind: workflow.ActionFinished, ActionID: "a", Status: workflow.Failed, Duration: time.Second}, // core.abort
			Line{Level: slog.LevelDebug, Step: "a", Msg: "failed in 1s", Detail: true}},
		{workflow.Event{Kind: workflow.ActionFinished, ActionID: "a", Status: workflow.Failed, Err: boom, Duration: 1234 * time.Millisecond},
			Line{Level: slog.LevelError, Step: "a", Msg: "failed after 1.23s: boom"}},
		{workflow.Event{Kind: workflow.ActionFinished, ActionID: "a", Status: workflow.Cancelled, Duration: 2 * time.Second},
			Line{Level: slog.LevelWarn, Step: "a", Msg: "cancelled after 2s"}},
		{workflow.Event{Kind: workflow.RunFinished, Status: workflow.Succeeded, Duration: time.Second}, Line{Level: slog.LevelInfo, Msg: `workflow "w" succeeded in 1s`}},
		{workflow.Event{Kind: workflow.RunFinished, Status: workflow.Failed, Err: boom, Duration: time.Second}, Line{Level: slog.LevelError, Msg: `workflow "w" failed after 1s: boom`}},
	}
	for _, tt := range tests {
		got, ok := Describe("w", "test", tt.e)
		if !ok || got != tt.want {
			t.Errorf("Describe(%v) = %+v, %v; want %+v", tt.e.Kind, got, ok, tt.want)
		}
	}
}

func TestDescribePipeline(t *testing.T) {
	tests := []struct {
		e    pipeline.Event
		want Line
	}{
		{pipeline.Event{Kind: pipeline.RunStarted, RunID: "R1"}, Line{Level: slog.LevelInfo, Msg: `pipeline "p" started, run R1`}},
		{pipeline.Event{Kind: pipeline.TransformFinished, TransformID: "t", Status: pipeline.Succeeded, Counts: pipeline.Counts{Read: 3, Written: 2}, Duration: time.Second},
			Line{Level: slog.LevelInfo, Step: "t", Msg: "read 3, written 2 in 1s"}},
		{pipeline.Event{Kind: pipeline.TransformFinished, TransformID: "t", Status: pipeline.Succeeded, Counts: pipeline.Counts{Read: 3, Written: 2, Info: 50}, Duration: time.Second},
			Line{Level: slog.LevelInfo, Step: "t", Msg: "read 3, written 2, lookup 50 in 1s"}},
		{pipeline.Event{Kind: pipeline.TransformFinished, TransformID: "t", Status: pipeline.Failed, Err: errors.New("boom"), Counts: pipeline.Counts{Read: 3, Written: 2, Errors: 1}, Duration: time.Second},
			Line{Level: slog.LevelError, Step: "t", Msg: "failed after 1s (read 3, written 2, errors 1): boom"}},
		{pipeline.Event{Kind: pipeline.RunFinished, Status: pipeline.Cancelled, Duration: time.Second}, Line{Level: slog.LevelWarn, Msg: `pipeline "p" cancelled after 1s`}},
	}
	for _, tt := range tests {
		got, ok := DescribePipeline("p", "", tt.e)
		if !ok || got != tt.want {
			t.Errorf("DescribePipeline(%v) = %+v, %v; want %+v", tt.e.Kind, got, ok, tt.want)
		}
	}
}

// TestForward checks the log of a called workflow in the log of the calling action:
// failures are warnings, the end of the run is only told on success.
func TestForward(t *testing.T) {
	var lines []string
	f := forward(func(level slog.Level, msg string) { lines = append(lines, fmt.Sprintf("%s %s", level, msg)) })
	for _, e := range []workflow.Event{
		{Kind: workflow.RunStarted, RunID: "R1", Message: "child"},
		{Kind: workflow.Log, ActionID: "a", Level: slog.LevelInfo, Message: "hi"},
		{Kind: workflow.ActionFinished, ActionID: "a", Status: workflow.Succeeded, Duration: time.Second},
		{Kind: workflow.ActionFinished, ActionID: "b", Status: workflow.Failed, Err: errors.New("boom"), Duration: time.Second},
		{Kind: workflow.RunFinished, Status: workflow.Failed, Err: errors.New("boom")},
	} {
		f(e)
	}
	want := []string{
		`DEBUG workflow "child" started, run R1`,
		"INFO child/a: hi",
		"DEBUG child/a: succeeded in 1s",
		"WARN child/b: failed after 1s: boom",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("got\n%q\nwant\n%q", lines, want)
	}
}
