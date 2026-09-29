package daemon

import (
	"fmt"
	"log"
	"strings"
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

// ArchiveWorkers archives every done worker card with no live runner.
func (d *Daemon) ArchiveWorkers(dryRun bool) (*ArchiveWorkersResult, error) {
	tasks, err := d.st.List("done")
	if err != nil {
		return nil, err
	}
	fixtures, err := d.st.Fixtures()
	if err != nil {
		return nil, err
	}
	fixtureCards := map[string]bool{}
	for _, f := range fixtures {
		if f.TaskID != "" {
			fixtureCards[f.TaskID] = true
		}
	}
	res := &ArchiveWorkersResult{DryRun: dryRun, Archived: []ArchivedWorker{}}
	for _, t := range tasks {
		switch {
		case !hasTag(t.Tags, OriginAgentTag), !d.isWorker(t):
			continue
		case t.Pinned, fixtureCards[t.ID]:
			res.Kept++
			continue
		case d.sup.get(t.ID) != nil:
			res.Kept++
			continue
		}
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
