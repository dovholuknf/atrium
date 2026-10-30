package daemon

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
)

// The one-time tidy: archive the done WORKER cards that piled up before merged
// workers were culled on their own. See docs/rnd/merged-cull-design.md.
//
// ARCHIVE ONLY. This reuses the store's ArchiveCulled, which takes a card off
// the board and keeps the row, its history and its ledger. It never calls Prune
// or Forget, and it removes no worktree and no branch: those are a cull's job,
// and a cull proves a merge first. This proves nothing, so it touches nothing on
// disk.
//
// WHAT IT WILL NOT TOUCH, whatever the flags: a director or the orchestrator, any
// card without `origin:agent`, a card that is not done, a card with a live
// runner, a pinned card, and a card a fixture starts. A worker is `atrium:subagent`
// or `origin:agent` with a recorded launcher and no `atrium:director` tag.

// ArchivedWorker is one card the tidy archived, or would.
type ArchivedWorker struct {
	Card  string `json:"card"`
	Title string `json:"title"`
}

// ArchiveWorkersResult is what one run did. DryRun means nothing was changed
// and Archived lists what would have been.
type ArchiveWorkersResult struct {
	DryRun   bool             `json:"dry_run"`
	Archived []ArchivedWorker `json:"archived"`
	Kept     int              `json:"kept"`
}

// SettingArchiveWorkersAfter is how long a done worker card stays before the sweep archives it, in seconds.
const SettingArchiveWorkersAfter = store.SettingArchiveWorkersAfter

// ArchiveWorkersDefaultAfter applies when the setting was never written.
const ArchiveWorkersDefaultAfter = 24 * time.Hour

// doneWorkers is the one selection the sweep and `archive-workers` share, so the two cannot disagree. take is the
// done worker cards to archive, kept the ones that matched but are held back (pinned, a fixture, a live runner).
// A positive olderThan also holds back a card that finished more recently than that, uncounted.
func (d *Daemon) doneWorkers(olderThan time.Duration) (take []*store.Task, kept int, err error) {
	tasks, err := d.st.List("done")
	if err != nil {
		return nil, 0, err
	}
	fixtures, err := d.st.Fixtures()
	if err != nil {
		return nil, 0, err
	}
	fixtureCards := map[string]bool{}
	for _, f := range fixtures {
		if f.TaskID != "" {
			fixtureCards[f.TaskID] = true
		}
	}
	cutoff := time.Now().Add(-olderThan)
	for _, t := range tasks {
		switch {
		case !small.HasTag(t.Tags, OriginAgentTag), !d.isWorker(t):
			continue
		case olderThan > 0 && t.LastActivityAt.After(cutoff):
			continue
		case t.Pinned, fixtureCards[t.ID]:
			kept++
			continue
		case d.sup.Get(t.ID) != nil:
			kept++
			continue
		}
		take = append(take, t)
	}
	return take, kept, nil
}

// archiveWorkersAfter reads the age, and whether the sweep archives done workers at all. A read failure answers off.
func (d *Daemon) archiveWorkersAfter() (time.Duration, bool) {
	v, err := d.st.Setting(SettingArchiveWorkersAfter)
	if err != nil {
		return 0, false
	}
	switch strings.TrimSpace(v) {
	case "":
		return ArchiveWorkersDefaultAfter, true
	case "off":
		return 0, false
	}
	secs, err := time.ParseDuration(strings.TrimSpace(v) + "s")
	if err != nil || secs <= 0 {
		return 0, false
	}
	return secs, true
}

// sweepDoneWorkers archives done worker cards that finished longer ago than the configured age.
func (d *Daemon) sweepDoneWorkers() error {
	after, ok := d.archiveWorkersAfter()
	if !ok {
		return nil
	}
	take, _, err := d.doneWorkers(after)
	if err != nil {
		return err
	}
	n := 0
	for _, t := range take {
		if err := d.st.ArchiveCulled(t.ID, "archived: a finished worker, past its time on the board"); err != nil {
			return fmt.Errorf("archiving %s: %w", t.DisplayTitle(), err)
		}
		n++
	}
	if n > 0 {
		log.Printf("[atrium] archived %d done worker card(s) older than %s", n, after)
		d.ap.Broadcast("task-removed", nil)
	}
	return nil
}

// ArchiveWorkers archives every done worker card with no live runner.
func (d *Daemon) ArchiveWorkers(dryRun bool) (*ArchiveWorkersResult, error) {
	take, kept, err := d.doneWorkers(0)
	if err != nil {
		return nil, err
	}
	res := &ArchiveWorkersResult{DryRun: dryRun, Archived: []ArchivedWorker{}, Kept: kept}
	for _, t := range take {
		if !dryRun {
			if err := d.st.ArchiveCulled(t.ID, "archived: a finished worker, tidied in one go"); err != nil {
				return res, fmt.Errorf("archiving %s: %w", t.DisplayTitle(), err)
			}
		}
		res.Archived = append(res.Archived, ArchivedWorker{Card: t.ID, Title: t.DisplayTitle()})
	}
	if !dryRun && len(res.Archived) > 0 {
		ids := make([]string, 0, len(res.Archived))
		for _, a := range res.Archived {
			ids = append(ids, a.Card)
		}
		log.Printf("[atrium] archived %d finished worker card(s): %s", len(ids), strings.Join(ids, ", "))
		d.ap.Broadcast("task-removed", nil)
	}
	return res, nil
}
