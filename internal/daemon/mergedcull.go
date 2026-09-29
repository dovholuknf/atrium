package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A merged worker is culled without anybody remembering to. See
// docs/rnd/merged-cull-design.md, which is the whole argument.
//
// The one trigger is a git `post-merge` hook running `atrium merged --into X`.
// This file answers it: find the workers the merge covered, MARK the ones that
// are finished, and tell their launcher once. The sweep then culls a marked
// worker when its time comes, with every check re-run, so a merge is still not
// an acceptance: the grace period is the window in which to send the work back.
//
// NOTHING HERE LOOKS FOR MERGES ON A TIMER. The hook is the only trigger, and
// the only timer is the per-card `cull_at`.
//
// EVERY REFUSAL HERE IS SILENT TO THE CALLER. `atrium merged` runs inside a git
// hook and must never fail a merge, so an error is logged and the answer is
// still 200.

// MergedGraceDefault is how long a merged worker stays before it is culled.
const MergedGraceDefault = 30 * time.Minute

// SettingMergedCullGrace is the daemon setting. Seconds, or `off`.
const SettingMergedCullGrace = store.SettingMergedCullGrace

// mergedGrace reads the grace period, and whether the feature is on. A read
// failure answers off: this ends up removing directories.
func (d *Daemon) mergedGrace() (time.Duration, bool) {
	v, err := d.st.Setting(SettingMergedCullGrace)
	if err != nil {
		return 0, false
	}
	switch strings.TrimSpace(v) {
	case "":
		return MergedGraceDefault, true
	case "off":
		return 0, false
	}
	secs, err := time.ParseDuration(strings.TrimSpace(v) + "s")
	if err != nil || secs <= 0 {
		return 0, false
	}
	return secs, true
}

// isWorker says whether a card is one atrium may cull: tagged `atrium:subagent`,
// or launched by an agent with a recorded launcher and not asked to be a
// director. A director is never a worker, tag or no tag.
func (d *Daemon) isWorker(t *store.Task) bool {
	if t == nil || hasTag(t.Tags, DirectorTag) {
		return false
	}
	if hasTag(t.Tags, SubagentTag) {
		return true
	}
	if !hasTag(t.Tags, OriginAgentTag) {
		return false
	}
	w, err := d.st.WorkItem(t.ID)
	return err == nil && strings.TrimSpace(w.LauncherID) != ""
}

// MergedResult is what one `merged --into` did.
type MergedResult struct {
	Into    string   `json:"into"`
	Marked  []string `json:"marked,omitempty"`
	Skipped []string `json:"skipped,omitempty"`
	Off     bool     `json:"off,omitempty"`
}

// handleMerged is the post-merge hook's request. Always 200.
func (d *Daemon) handleMerged(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Into string `json:"into"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in)
	res, err := d.Merged(in.Into)
	if err != nil {
		log.Printf("[atrium] merged --into %s: %v", in.Into, err)
		res = &MergedResult{Into: in.Into}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// Merged marks the finished workers a merge into `into` covered. The candidates
// are workers whose launcher's worktree is on `into`, or every worker when
// `into` is claude/main. Each is checked with the same read Cull makes.
func (d *Daemon) Merged(into string) (*MergedResult, error) {
	into = strings.TrimSpace(into)
	if into == "" {
		return nil, fmt.Errorf("no branch named")
	}
	res := &MergedResult{Into: into}
	grace, on := d.mergedGrace()
	if !on {
		res.Off = true
		return res, nil
	}
	items, err := d.st.OpenWorkItems()
	if err != nil {
		return nil, err
	}
	for _, w := range items {
		if reason := d.markIfMerged(w, into, grace); reason != "" {
			res.Skipped = append(res.Skipped, w.TaskID+": "+reason)
		} else {
			res.Marked = append(res.Marked, w.TaskID)
		}
	}
	if len(res.Marked) > 0 {
		log.Printf("[atrium] merged into %s: marked %s, to be culled in %s",
			into, strings.Join(res.Marked, ", "), grace)
	}
	return res, nil
}

// markIfMerged marks one worker and returns "" when it did, or why it did not.
func (d *Daemon) markIfMerged(w *store.WorkItem, into string, grace time.Duration) string {
	t, err := d.st.Get(w.TaskID)
	if err != nil {
		return "no card"
	}
	if !d.isWorker(t) {
		return "not a worker"
	}
	if w.HeldBy != "" {
		return "held"
	}
	if w.CullAt != nil {
		return "already marked"
	}
	if strings.TrimSpace(t.Worktree) == "" {
		return "no directory"
	}
	if into != DefaultCullInto && !d.launcherOn(w, into) {
		return "its launcher is not on " + into
	}
	if reason := d.notFinished(t, w); reason != "" {
		return reason
	}
	plan, err := inspectCull(t.Worktree, into)
	if err != nil {
		return err.Error()
	}
	if !plan.merged {
		return "branch " + plan.branch + " is not merged into " + into
	}
	sha, err := gitIn(plan.common, "--git-dir="+plan.common, "rev-parse", "--verify", "-q", "refs/heads/"+plan.branch)
	if err != nil {
		return err.Error()
	}
	at := time.Now().Add(grace)
	marked, err := d.st.MarkMerged(w.TaskID, into, sha, plan.branch, at)
	if err != nil {
		return err.Error()
	}
	if !marked {
		return "not markable"
	}
	d.publishTask(w.TaskID)
	d.tellLauncherMerged(t, w, into, sha, at)
	return ""
}

// notFinished is why a worker is not ready to be marked, or "": finished status,
// its last report `done`, no turn in progress and no open question.
func (d *Daemon) notFinished(t *store.Task, w *store.WorkItem) string {
	if !finishedStatus(t.Status) {
		return "still " + t.Status
	}
	full, err := d.st.WorkItem(t.ID)
	if err != nil {
		return "no work item"
	}
	if full.LastReport == nil || full.LastReport.Status != "done" {
		return "its last report was not done"
	}
	seen, err := d.st.GetSeen(t.ID)
	if err != nil {
		return "could not read its questions"
	}
	if seen.View().OpenCount() != 0 {
		return "it has an open question"
	}
	return ""
}

// launcherOn says whether a worker's launcher is working on branch `into`.
func (d *Daemon) launcherOn(w *store.WorkItem, into string) bool {
	if w.LauncherID == "" {
		return false
	}
	l, err := d.st.Get(w.LauncherID)
	if err != nil || strings.TrimSpace(l.Worktree) == "" {
		return false
	}
	branch, err := gitIn(filepath.FromSlash(l.Worktree), "symbolic-ref", "--short", "-q", "HEAD")
	return err == nil && strings.EqualFold(branch, into)
}

// tellLauncherMerged is the one message the launcher gets, delivered at its turn
// end and not mid-turn: it is news, not an interruption.
func (d *Daemon) tellLauncherMerged(t *store.Task, w *store.WorkItem, into, sha string, at time.Time) {
	if w.LauncherID == "" {
		return
	}
	l, err := d.st.Get(w.LauncherID)
	if err != nil {
		return
	}
	who := orDefault(t.WireName, t.ID)
	text := fmt.Sprintf("%s's branch merged into %s at %s. It will be culled at %s. "+
		"To keep it, atrium_cull card=%s hold=true.",
		who, into, shortRef(sha), at.Local().Format("15:04"), t.ID)
	if _, _, err := d.deliverPeerWhenID(l, "atrium", text, true); err != nil {
		log.Printf("[atrium] could not tell %s that %s merged: %v", l.DisplayTitle(), who, err)
	}
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// HoldCull is the hold: `atrium_cull hold=true` and the chip's keep. The mark
// goes and nothing marks the card again.
func (d *Daemon) HoldCull(taskID, by string) error {
	t, err := d.st.Get(taskID)
	if err != nil {
		return err
	}
	if err := d.st.HoldCull(taskID, by); err != nil {
		return fmt.Errorf("%s has no work item to hold: %w", t.DisplayTitle(), err)
	}
	d.publishTask(taskID)
	return nil
}

// sweepMergedCulls culls each marked worker whose time has come. Run from the
// reap tick, and off the tick itself because a cull waits out a worker's exit.
func (d *Daemon) sweepMergedCulls() {
	if _, on := d.mergedGrace(); !on {
		return
	}
	due, err := d.st.DueCulls(time.Now())
	if err != nil || len(due) == 0 {
		return
	}
	if !d.mergedCulling.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer d.mergedCulling.Store(false)
		for _, w := range due {
			d.cullDue(w)
		}
	}()
}

// cullDue culls one due worker with every check re-run. A check that no longer
// holds drops the mark rather than leaving it to fire again every tick.
func (d *Daemon) cullDue(w *store.WorkItem) {
	t, err := d.st.Get(w.TaskID)
	if err != nil {
		_, _ = d.st.ClearCullMark(w.TaskID)
		return
	}
	drop := func(why string) {
		log.Printf("[atrium] not culling %s at its time: %s", t.DisplayTitle(), why)
		_, _ = d.st.ClearCullMark(w.TaskID)
		d.publishTask(w.TaskID)
	}
	if !d.isWorker(t) {
		drop("it is not a worker")
		return
	}
	if why := d.notFinished(t, w); why != "" {
		drop(why)
		return
	}
	if _, err := d.Cull(w.TaskID, w.MergedInto); err != nil {
		drop(err.Error())
		return
	}
	d.publishTask(w.TaskID)
	d.ap.Broadcast("task-removed", nil)
}
