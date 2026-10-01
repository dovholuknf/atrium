package daemon

import (
	"log"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A launched worker that reports done is asked to leave.
//
// Nothing ended a worker after its final report: it had to call exit itself, and workers do not, so finished
// cards sat on the board with a live process until somebody exited them by hand. See
// docs/backlog/runtime/r-new-exit-on-report.md.
//
// Only a final `done`. A question, progress or blocked report changes nothing. A card with no launcher, a
// director and the orchestrator are never exited. No worktree and no branch is touched: whether the work lands
// is the launcher's call.
//
// The exit is the one `POST /v1/tasks/{id}/exit` runs, so the card stays `done` and keeps its report. It runs
// after the report is recorded, and the notice to the launcher is queued in that same transaction, so the
// launcher's copy is durable before the runner is asked to go.

// exitOnReportDelay is how long the worker is left running after its report is recorded, so the launcher's
// delivery is under way before the worker's terminal goes. A variable so a test does not wait.
var exitOnReportDelay = 5 * time.Second

// stopAfterReport is the exit itself. A variable so a test can watch it without owning a terminal.
var stopAfterReport = func(d *Daemon, taskID string) error { return d.StopRunner(taskID) }

// exitsOnReport says whether this report ends the card's runner. A done report that carries an ask is a question
// in a done report's clothes: the launcher's answer has to reach a running card, so it stays.
//
// Residents are exempt by the tags they already wear: a director, the orchestrator, and a card that opted in to
// parking when idle or to holding notices. A resident an agent launched with none of those tags is exited on its
// first done report, and the launcher puts one of them on it to prevent that.
func (d *Daemon) exitsOnReport(task *store.Task, in FinishRequest) bool {
	if task == nil || (in.Status != "" && in.Status != ReportDone) || strings.TrimSpace(in.Ask) != "" {
		return false
	}
	if strings.TrimSpace(task.SpawnedByID) == "" {
		return false
	}
	for _, tag := range []string{DirectorTag, OrchestratorTag, ParkIdleTag, HoldNoticesTag} {
		if hasTag(task.Tags, tag) {
			return false
		}
	}
	return true
}

// exitAfterReport waits out the delay, then asks the runner to leave. Best effort like every hook: a failure is
// logged and the report has already landed. An operator who typed into the terminal lately is left alone, by the
// same quiet period a ceiling card waits for.
func (d *Daemon) exitAfterReport(taskID string) {
	time.Sleep(exitOnReportDelay)
	if run := d.sup.get(taskID); run != nil && run.typedWithin(autoTiming.ceilingTypedQuiet) {
		log.Printf("[atrium] %s reported done and was typed in lately, so it keeps running", taskID)
		return
	}
	if err := stopAfterReport(d, taskID); err != nil {
		log.Printf("[atrium] could not exit %s after its done report: %v", taskID, err)
	}
}
