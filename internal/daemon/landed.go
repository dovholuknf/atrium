package daemon

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
)

// A worker whose work landed is reclaimed without its launcher remembering to. See docs/changes/b-landed-workers-never-reclaimed.md.
//
// LANDED IS BY PATCH ID AS WELL AS ANCESTRY. Landing rewrites shas (a cherry-pick onto claude/main, then a re-signed
// copy onto origin's main), so a branch whose every patch is on main is merged though it is not an ancestor. The proof
// is against claude/main, the hub's copy of it and origin/main, and any one carrying the branch is enough.
//
// THE SWEEP reclaims an agent-launched worker that is landed, idle for merged_cull_grace and sitting at needs-input or
// done, whether or not it ever said done. It is asked to leave, its worktree is removed only when clean and its branch
// pruned, by the same cull an explicit atrium_cull runs. A card the operator started is never in scope. A landed card
// that is kept, by a tag, a hold, an open question or a dirty worktree, says so on the board through landedKeptFor.

// landedRecheck is how often one card's landing is read again: every read is a few git commands.
const landedRecheck = 10 * time.Minute

type landedState struct {
	mu      sync.Mutex
	kept    map[string]string
	checked map[string]time.Time
	exitAt  map[string]time.Time
}

// exited records when a card's runner went, which is when its prune grace starts.
func (s *landedState) exited(id string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exitAt == nil {
		s.exitAt = map[string]time.Time{}
	}
	s.exitAt[id] = at
}

func (s *landedState) exitedAt(id string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitAt[id]
}

// SettingLandedReclaimGrace is how long a landed worker waits after its runner exits before it is pruned. Seconds, or
// `off`.
const SettingLandedReclaimGrace = store.SettingLandedReclaimGrace

// landedGrace is the prune grace, and whether pruning is on at all. merged_cull_grace `off` turns the whole of it off.
func (d *Daemon) landedGrace() (time.Duration, bool) {
	if _, on := d.mergedGrace(); !on {
		return 0, false
	}
	v, err := d.st.Setting(SettingLandedReclaimGrace)
	if err != nil {
		return 0, false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		v = store.DefaultLandedReclaimGrace
	}
	if v == "off" {
		return 0, false
	}
	secs, err := time.ParseDuration(v + "s")
	if err != nil || secs <= 0 {
		return 0, false
	}
	return secs, true
}

// launchedWork says whether a card was launched as work: by an agent, or over /v1/launch as a work lifetime or an item,
// whoever launched it. A card an operator started by hand is none of these.
func launchedWork(t *store.Task) bool {
	return t != nil && (agentLaunched(t) || t.Lifetime == store.LifetimeWork || isItem(t))
}

func (s *landedState) set(id, why string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.kept == nil {
		s.kept = map[string]string{}
	}
	if s.kept[id] == why {
		return false
	}
	if why == "" {
		delete(s.kept, id)
	} else {
		s.kept[id] = why
	}
	return true
}

// due says whether a card's landing is read now, and stamps it.
func (s *landedState) due(id string, now time.Time, every time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.checked == nil {
		s.checked = map[string]time.Time{}
	}
	if at, ok := s.checked[id]; ok && now.Sub(at) < every {
		return false
	}
	s.checked[id] = now
	return true
}

// landedKeptFor is why a landed card is still here, or nil.
func (d *Daemon) landedKeptFor(id string) any {
	d.landed.mu.Lock()
	defer d.landed.mu.Unlock()
	if why := d.landed.kept[id]; why != "" {
		return why
	}
	return nil
}

// landedNames are the refs a branch is proved landed against, after its own: `into`, and for claude/main the hub's copy
// and origin/main.
func landedNames(branch, into string) []string {
	names := []string{"refs/heads/" + branch, "refs/heads/" + into}
	if strings.EqualFold(into, DefaultCullInto) {
		names = append(names, "refs/remotes/hub/claude/main", "refs/remotes/origin/main")
	}
	return names
}

// landedInto says whether every commit of branch is on `into`: by ancestry, or by patch id (subjects alone are not
// enough to delete on). dir is any checkout or git dir of the repository.
func landedInto(dir, branch, into string) (bool, error) {
	refs, err := readRefs(dir, landedNames(branch, into)...)
	if err != nil {
		return false, err
	}
	merged, _, err := landedWith(dir, branch, into, refs)
	return merged, err
}

// landedWith is landedInto over refs already read, and says which ref proved it.
func landedWith(dir, branch, into string, have map[string]string) (bool, string, error) {
	var refs []string
	for _, r := range landedNames(branch, into)[1:] {
		if have[r] != "" {
			refs = append(refs, r)
		}
	}
	if len(refs) == 0 {
		return false, "", nil
	}
	head := "refs/heads/" + branch
	tip := have[head]
	if tip == "" {
		return false, "", fmt.Errorf("branch %s is not there", branch)
	}
	for _, ref := range refs {
		ok, err := isAncestor(dir, head, ref)
		if err != nil {
			if ref == "refs/heads/"+into {
				return false, "", err
			}
			continue
		}
		if ok {
			return true, ref, nil
		}
	}
	run := gitsync.NewRunner()
	defer run.Stop(time.Second)
	run.Bound = cullGitTimeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*cullGitTimeout)
	defer cancel()
	for _, ref := range refs {
		got, err := gitsyncLanded(ctx, run, dir, ref, tip)
		if err == nil && got {
			return true, ref, nil
		}
	}
	return false, "", nil
}

func gitsyncLanded(ctx context.Context, run *gitsync.Runner, dir, ref, tip string) (bool, error) {
	res, err := gitsync.LandedBy(ctx, run, dir, nil, ref, []string{tip})
	if err != nil {
		return false, err
	}
	l, ok := res[tip]
	return ok && l.Unlanded == 0 && (l.How == gitsync.HowAncestry || l.How == gitsync.HowPatchID), nil
}

// reclaimBlockIdle is reclaimBlock for the idle sweep: the card need not have said done, but it has finished a turn and
// asks nothing, and it was not held or tagged to stay.
func (d *Daemon) reclaimBlockIdle(t *store.Task) string {
	switch {
	case t == nil:
		return "there is no card"
	case !launchedWork(t):
		return "an agent did not launch it, so atrium never reclaims it"
	case staysOpen(t):
		return "it is tagged to stay open or is an investigation. take the tag off to reclaim it"
	case t.Status == store.StatusDone:
		if why := d.reclaimBlock(t); why != "" {
			return why
		}
	case t.Status != store.StatusNeedsInput || t.WaitingReason == "started":
		return "its work is not done (" + t.Status + "), so it stays in Terminals"
	}
	if w, err := d.st.WorkItem(t.ID); err == nil && w.HeldBy != "" {
		return "it is held by " + w.HeldBy
	}
	if seen, err := d.st.GetSeen(t.ID); err != nil || seen.View().OpenCount() != 0 {
		return "it has an open question"
	}
	return ""
}

// sweepLandedIdle reads the landing of each candidate worker and reclaims the idle ones. Off the tick: it runs git.
func (d *Daemon) sweepLandedIdle() {
	grace, on := d.landedGrace()
	if !on {
		return
	}
	if !d.landedSweeping.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer d.landedSweeping.Store(false)
		d.landedPass(grace, time.Now())
	}()
}

func (d *Daemon) landedPass(grace time.Duration, now time.Time) {
	tasks, err := d.st.List()
	if err != nil {
		return
	}
	every := grace / 2
	if every < 30*time.Second {
		every = 30 * time.Second
	}
	if every > landedRecheck {
		every = landedRecheck
	}
	for _, t := range tasks {
		if t.ArchivedAt != nil || !launchedWork(t) || !d.isWorker(t) || strings.TrimSpace(t.Worktree) == "" {
			continue
		}
		if t.Status != store.StatusDone && t.Status != store.StatusNeedsInput {
			d.landedSet(t.ID, "")
			continue
		}
		if !d.landed.due(t.ID, now, every) || d.Nctx.Holding(t.ID) {
			continue
		}
		d.landedOne(t, grace, now)
	}
}

func (d *Daemon) landedSet(id, why string) {
	if d.landed.set(id, why) {
		d.Publish(id)
	}
}

func (d *Daemon) landedOne(t *store.Task, grace time.Duration, now time.Time) {
	if !hasDotGit(t.Worktree) {
		return
	}
	idleSince := t.LastActivityAt
	if at := d.landed.exitedAt(t.ID); at.After(idleSince) {
		idleSince = at
	}
	plan, err := inspectCull(t.Worktree, DefaultCullInto)
	if err != nil || !plan.merged {
		if err == nil && now.Sub(idleSince) >= grace {
			d.keepLanded(t, "not pruned: "+plan.branch+" has commits not on "+DefaultCullInto)
		} else {
			d.landedSet(t.ID, "")
		}
		return
	}
	switch why := d.landedHeld(t, plan); {
	case why != "":
		d.keepLanded(t, "landed, kept: "+why)
		return
	case now.Sub(idleSince) < grace || d.DeployHeld(t.ID):
		d.landedSet(t.ID, "")
		return
	}
	res, err := d.cullProved(t.ID, DefaultCullInto, "", true, plan)
	if err != nil {
		log.Printf("[atrium] not reclaiming landed %s: %v", t.DisplayTitle(), err)
		d.keepLanded(t, "landed, kept: "+err.Error())
		return
	}
	d.landed.set(t.ID, "")
	line := fmt.Sprintf("pruned %s: worktree removed=%v branch %s deleted=%v", t.Worktree, res.WorktreeRemoved,
		res.Branch, res.BranchDeleted)
	if res.Kept != "" {
		line += ", kept: " + res.Kept
	}
	d.noteOnCard(t.ID, line)
	d.Publish(t.ID)
	d.ap.Broadcast("task-removed", nil)
	log.Printf("[atrium] %s (%s) after %s idle", line, t.DisplayTitle(), grace)
}

// keepLanded says on the board, and once on the card, why a landed worker is not pruned.
func (d *Daemon) keepLanded(t *store.Task, why string) {
	if d.landed.set(t.ID, why) {
		d.noteOnCard(t.ID, why)
		d.Publish(t.ID)
	}
}

func (d *Daemon) noteOnCard(id, line string) {
	if err := d.st.AppendEvent(id, store.EventNotified, map[string]any{"by": "prune", "text": line}); err != nil {
		log.Printf("[atrium] could not note on %s: %v", id, err)
	}
}

// landedHeld is why a landed worker is kept, or "".
func (d *Daemon) landedHeld(t *store.Task, plan *cullPlan) string {
	switch {
	case small.HasTag(t.Tags, KeepOpenTag):
		return "tagged " + KeepOpenTag
	case small.HasTag(t.Tags, InvestigationTag):
		return "tagged " + InvestigationTag
	case staysOpen(t):
		return "tagged to stay open"
	case len(plan.dirty) > 0:
		return fmt.Sprintf("the worktree has %d uncommitted changes", len(plan.dirty))
	}
	if why := d.reclaimBlockIdle(t); why != "" {
		return why
	}
	return ""
}
