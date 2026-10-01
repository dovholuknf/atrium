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

// exitsOnReport says whether this report ends the card's runner. status is the report's status as sent.
func (d *Daemon) exitsOnReport(task *store.Task, status string) bool {
	if task == nil || (status != "" && status != ReportDone) {
		return false
	}
	if strings.TrimSpace(task.SpawnedByID) == "" {
		return false
	}
	return !hasTag(task.Tags, DirectorTag) && !hasTag(task.Tags, OrchestratorTag)
}

// exitAfterReport waits out the delay, then asks the runner to leave. Best effort like every hook: a failure is
// logged and the report has already landed.
func (d *Daemon) exitAfterReport(taskID string) {
	time.Sleep(exitOnReportDelay)
	if err := stopAfterReport(d, taskID); err != nil {
		log.Printf("[atrium] could not exit %s after its done report: %v", taskID, err)
	}
}
