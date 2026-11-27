package runs

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/runnelhq/runnel/internal/engine/pipeline"
	"github.com/runnelhq/runnel/internal/engine/workflow"
)

// Line is an event of a run in words: the one place for the texts of the run log of
// "runnel run" and of runs called by core.run-workflow and core.run-pipeline.
type Line struct {
	Level slog.Level
	// Step is the action or transform; empty for the run itself.
	Step string
	Msg  string
	// Detail marks lines shown only on request ("runnel run -v").
	Detail bool
}

// Describe puts an event of a workflow run into words; name is the workflow, env the
// environment (empty: none). false: the event has no line (outputs are left to the
// caller).
func Describe(name, env string, e workflow.Event) (Line, bool) {
	l := Line{Level: slog.LevelInfo, Step: e.ActionID}
	switch e.Kind {
	case workflow.RunStarted:
		l.Msg = started("workflow", name, env, e.RunID)
	case workflow.ActionStarted:
		l.Level, l.Detail, l.Msg = slog.LevelDebug, true, "started ("+e.ActionType+")"
	case workflow.Log:
		l.Level, l.Detail, l.Msg = e.Level, e.Level < slog.LevelInfo, e.Message
	case workflow.ActionWaiting:
		l.Level, l.Detail, l.Msg = slog.LevelDebug, true, e.Message
	case workflow.ActionSkipped:
		l.Level, l.Detail, l.Msg = slog.LevelDebug, true, "skipped ("+workflow.SkipReason+")"
	case workflow.ActionFinished:
		if e.Err == nil && e.Status != workflow.Cancelled {
			// Also an action that ends the run on purpose (core.abort: failed, no error).
			l.Level, l.Detail, l.Msg = slog.LevelDebug, true, fmt.Sprintf("%s in %s", e.Status, Round(e.Duration))
			break
		}
		l.Level, l.Msg = ending(string(e.Status), e.Duration, "", e.Err)
	case workflow.RunFinished:
		l.Step = ""
		l.Level, l.Msg = ending(string(e.Status), e.Duration, "", e.Err)
		l.Msg = fmt.Sprintf("workflow %q %s", name, l.Msg)
	default:
		return Line{}, false
	}
	return l, true
}

// DescribePipeline puts an event of a pipeline run into words, like Describe. Every
// transform ends with its counts, so that the numbers are visible without details.
func DescribePipeline(name, env string, e pipeline.Event) (Line, bool) {
	l := Line{Level: slog.LevelInfo, Step: e.TransformID}
	switch e.Kind {
	case pipeline.RunStarted:
		l.Msg = started("pipeline", name, env, e.RunID)
	case pipeline.TransformStarted:
		l.Level, l.Detail, l.Msg = slog.LevelDebug, true, "started ("+e.TransformType+")"
	case pipeline.Log:
		l.Level, l.Detail, l.Msg = e.Level, e.Level < slog.LevelInfo, e.Message
	case pipeline.TransformFinished:
		counts := fmt.Sprintf("read %d, written %d", e.Counts.Read, e.Counts.Written)
		if e.Counts.Errors > 0 {
			counts += fmt.Sprintf(", errors %d", e.Counts.Errors)
		}
		if e.Counts.Info > 0 {
			counts += fmt.Sprintf(", lookup %d", e.Counts.Info)
		}
		l.Level, l.Msg = ending(string(e.Status), e.Duration, counts, e.Err)
		if l.Level < slog.LevelWarn {
			l.Msg = fmt.Sprintf("%s in %s", counts, Round(e.Duration))
		}
	case pipeline.RunFinished:
		l.Step = ""
		l.Level, l.Msg = ending(string(e.Status), e.Duration, "", e.Err)
		l.Msg = fmt.Sprintf("pipeline %q %s", name, l.Msg)
	default:
		return Line{}, false
	}
	return l, true
}

func started(kind, name, env, runID string) string {
	where := ""
	if env != "" {
		where = fmt.Sprintf(" (environment %s)", env)
	}
	return fmt.Sprintf("%s %q started%s, run %s", kind, name, where, runID)
}

// ending words the end of a run or a step: "succeeded in 1.2s", "cancelled after 3s
// (read 4, written 2)", "failed after 3s: …". counts are added in brackets if given.
func ending(status string, d time.Duration, counts string, err error) (slog.Level, string) {
	if counts != "" {
		counts = " (" + counts + ")"
	}
	switch {
	case status == string(workflow.Cancelled):
		return slog.LevelWarn, fmt.Sprintf("cancelled after %s%s", Round(d), counts)
	case status == string(workflow.Failed) || err != nil:
		return slog.LevelError, fmt.Sprintf("failed after %s%s: %v", Round(d), counts, err)
	}
	return slog.LevelInfo, fmt.Sprintf("succeeded in %s", Round(d))
}

// Round shortens a duration for messages: milliseconds below a second, else
// hundredths of a second.
func Round(d time.Duration) time.Duration {
	if d < time.Second {
		return d.Round(time.Millisecond)
	}
	return d.Round(10 * time.Millisecond)
}
