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
//
// A scratch launch is exited like any other: it gets no keep-open tag of its own, so it never holds a launch slot after
// its done report and `atrium_cull` can reclaim its directory. A launcher that wants one open passes the tag.

// exitOnReportDelay is how long the worker is left running after its report is recorded, so the launcher's
// delivery is under way before the worker's terminal goes. A variable so a test does not wait.
var exitOnReportDelay = 5 * time.Second

// stopAfterReport is the exit itself. A variable so a test can watch it without owning a terminal.
var stopAfterReport = func(d runnerStopper, taskID string) error { return d.StopRunnerBy(taskID, "atrium after its report") }

// exitsOnReport says whether this report ends the card's runner. A done report that carries an ask is a question
// in a done report's clothes: the launcher's answer has to reach a running card, so it stays.
//
// A card launched with lifetime `work` exits whoever launched it, since a launch through the hub's HTTP API is
// recorded as the operator's.
//
// Residents are exempt by the tags they already wear: a director, the orchestrator, and a card that opted in to
// parking when idle or to holding notices. A resident an agent launched with none of those tags is exited on its
// first done report, and the launcher puts one of them on it to prevent that.
func (d *Daemon) exitsOnReport(task *store.Task, in FinishRequest) bool {
	if task == nil || (in.Status != "" && in.Status != ReportDone) || strings.TrimSpace(in.Ask) != "" {
		return false
	}
	if task.Lifetime != store.LifetimeWork && strings.TrimSpace(task.SpawnedByID) == "" && !launchedByAnAgent(task) {
		return false
	}
	for _, tag := range []string{ResidentTag, OrchestratorTag, ParkIdleTag, HoldNoticesTag} {
		if hasTag(task.Tags, tag) {
			return false
		}
	}
	// A per-item minion is launched as a director so it can end itself, and it has nothing to do once it has
	// landed: it exits on done like any worker. See docs/changes/bug-idle-minion-spins-a-core.md.
	if hasTag(task.Tags, DirectorTag) && !hasTag(task.Tags, MinionTag) {
		return false
	}
	if isLinkCard(task) || staysOpen(task) {
		return false
	}
	return true
}

// launchedByAnAgent says whether a card has a launcher that is a session, even when the launcher's handle did not
// resolve to a card id at launch, which leaves SpawnedByID empty.
func launchedByAnAgent(t *store.Task) bool {
	by := strings.TrimSpace(t.SpawnedBy)
	return agentLaunched(t) && by != "" && by != store.HumanLauncher
}

// ResidentTag marks a card that stays open between tasks: a done or a blocked reports a task and never ends it. The
// same tag as link.ResidentTag.
const ResidentTag = "atrium:resident"

// MinionTag marks a per-item minion: a director that exits on its final done.
const MinionTag = "atrium:minion"

// isResident says whether a set of tags is a resident card's: the tag itself, or a director or the orchestrator.
func isResident(t []string) bool {
	return hasTag(t, ResidentTag) || hasTag(t, DirectorTag) || hasTag(t, OrchestratorTag)
}

// KeepOpenTag and InvestigationTag are the two tags that say a card is not closed by a done report and is never
// reclaimed. The launcher puts one on when the card is meant to outlive its first answer: a long conversation, or
// an investigation whose transcript is the product. See reclaim.go.
const (
	KeepOpenTag      = "atrium:keep-open"
	InvestigationTag = "atrium:investigation"
)

// staysOpen says whether a card is tagged to stay open or is an investigation.
func staysOpen(t *store.Task) bool {
	return t != nil && (hasTag(t.Tags, KeepOpenTag) || hasTag(t.Tags, InvestigationTag) || hasTag(t.Tags, ResidentTag))
}

// LinkTagPrefix marks a card a link opened (`link:<key>`, internal/api/open.go). Its lifetime is the link's: a done
// report does not end it, and only a close or the operator does. See docs/rnd/card-lifecycle-design.md section 7.
const LinkTagPrefix = "link:"

func isLinkCard(t *store.Task) bool {
	for _, tag := range t.Tags {
		if strings.HasPrefix(tag, LinkTagPrefix) {
			return true
		}
	}
	return false
}

// reportTypedQuiet is how long since a person last typed into the card for it to be left to exit.
var reportTypedQuiet = 2 * time.Minute

// exitReportWhy is what a done report and the card's event say about the runner leaving.
const exitReportWhy = "it reported done and was not launched resident. the card, worktree and branch stay, and its launcher " +
	"can resume it with a say that passes wake"

// exitAfterReport waits out the delay, then asks the runner to leave. Best effort like every hook: a failure is
// logged and the report has already landed. An operator who typed into the terminal lately is left alone, by the
// quiet period, reportTypedQuiet.
func (d *Daemon) exitAfterReport(taskID string) {
	time.Sleep(d.hk.exitOnReportDelay)
	if run := d.sup.get(taskID); run != nil && run.typedWithin(reportTypedQuiet) {
		log.Printf("[atrium] %s reported done and was typed in lately, so it keeps running", taskID)
		return
	}
	if err := d.st.AppendEvent(taskID, store.EventNotified, map[string]any{"by": "exit-on-report", "why": exitReportWhy}); err != nil {
		log.Printf("[atrium] could not record why %s exits after its report: %v", taskID, err)
	}
	if err := d.hk.stopAfterReport(d, taskID); err != nil {
		log.Printf("[atrium] could not exit %s after its done report: %v", taskID, err)
	}
}

// exitHooks are the seams of the exit on report.
type exitHooks struct {
	exitOnReportDelay time.Duration
	stopAfterReport   func(r runnerStopper, taskID string) error
}

// runnerStopper is the part of the daemon the exit asks the runner to go through.
type runnerStopper interface {
	StopRunnerBy(taskID, by string) error
}

func defaultExitHooks() exitHooks {
	return exitHooks{exitOnReportDelay: exitOnReportDelay, stopAfterReport: stopAfterReport}
}
