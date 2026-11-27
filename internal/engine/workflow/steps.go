package workflow

import (
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/model"
)

// SkipReason is the reason the engine gives for a skipped action (ADR 0003).
const SkipReason = "no incoming link applies"

// StepRecorder receives the data of the steps of a run: what each action got and
// returned (E23a). The runs package sets it only for the runs started in the editor;
// without it (nil) the engine collects nothing.
type StepRecorder interface {
	// Begin is called for each execution of an action (loops and join each run it again).
	Begin(a *model.Action) StepTrace
	// Skipped is called when an action is skipped, with the reason.
	Skipped(a *model.Action, reason string)
}

// StepTrace collects the data of one execution of an action. Attempt and Entry may be
// called by several goroutines at the same time (forEach with parallel).
type StepTrace interface {
	// Entries is called with the number of entries of forEach before they run.
	Entries(n int)
	// Attempt is called after each attempt: entry is the number of the entry of forEach
	// (from 1; 0 without forEach) and row its value, with the configuration as it reached
	// the action (nil if it could not be resolved), res and err what the action returned.
	Attempt(entry int, row any, attempt int, with map[string]any, res action.Result, err error)
	// Entry is called when an entry of forEach has ended (cancelled: stopped because
	// another entry failed).
	Entry(entry int, status Status)
	// End is called when the execution has ended, with the result of the action (for
	// forEach the list of the entries).
	End(status Status, d time.Duration, res action.Result, err error)
}
