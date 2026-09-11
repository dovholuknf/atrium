package daemon

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

// TERMINALS THAT WERE OPEN COME BACK AFTER A RESTART.
//
// Before this, only fixtures did. A fixture is a standing decision, "this
// terminal exists every day", and everything else was left behind: a card
// launched from a pull request, an unshelved piece of work, anything started
// by hand. The daemon stopped, four of six terminals came back, and the two
// that did not were the two nobody had written a fixture for.
//
// That is the wrong shape for a restart. A restart is not a decision about
// what work matters. It is maintenance, and the operator's own words for what
// it cost were "we keep restarting and i keep losing shit".
//
// WHAT IS RECORDED IS ONLY THE LIST OF CARDS. Everything needed to start one
// again is already on its row: the harness is `Runner`, the directory is
// `Worktree`, and the conversation to pick back up is `ResumeID`. Copying
// those into a file would be a second source of truth that goes stale the
// first time somebody retitles a card or moves a worktree.
//
// This sits beside `carryover.go` on purpose and the pair is the whole
// feature: carryover brings back what the terminal SAID, this brings back the
// terminal. Neither is any use without the other.

// reopenFile is written into the same directory as the scrollback, which is
// beside the database. One place for "what the last daemon left behind".
const reopenFile = "reopen.json"

// reopenGap is the pause between starting one and the next, and the reason is
// the one `fixtureGap` gives: half a dozen runners resolving a harness and
// opening a pty in the same instant is a stampede somebody watches.
const reopenGap = 400 * time.Millisecond

// reopenRecord is what the last daemon had open.
//
// A `saved_at` nobody reads yet, because the first question asked of a file
// like this is always "how old is this", and answering it later means a format
// change.
type reopenRecord struct {
	SavedAt time.Time `json:"saved_at"`
	Cards   []string  `json:"cards"`
}

func (d *Daemon) reopenPath() string {
	return filepath.Join(filepath.Dir(d.opts.DBPath), reopenFile)
}

// saveReopen records which cards had a runner atrium owned.
//
// Called from the wind-down beside `saveCarryover`, and best effort in the
// same way: every failure is logged and stepped over. A shutdown must not fail
// or hang over this, and the cost of it not being written is the behaviour
// that existed before it did.
//
// WRITTEN EVEN WHEN THE LIST IS EMPTY, and that is deliberate. An empty file
// means "the last daemon had nothing open", which is a different fact from no
// file at all, and leaving yesterday's list in place would reopen terminals
// somebody closed on purpose before stopping.
func (d *Daemon) saveReopen(rs []*runner) {
	rec := reopenRecord{SavedAt: time.Now()}
	for _, r := range rs {
		if r != nil && r.taskID != "" {
			rec.Cards = append(rec.Cards, r.taskID)
		}
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		log.Printf("[atrium] could not record what was open: %v", err)
		return
	}
	path := d.reopenPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("[atrium] could not make a place to record what was open: %v", err)
		return
	}
	// Written aside and renamed, so a daemon killed mid-write leaves the last
	// good list rather than half of this one.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		log.Printf("[atrium] could not record what was open: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		log.Printf("[atrium] could not record what was open: %v", err)
		return
	}
	log.Printf("[atrium] recorded %d open terminal(s) to reopen", len(rec.Cards))
}

// readReopen returns the cards the last daemon had open, or nothing.
//
// NOTHING FOR EVERYTHING UNREADABLE, the posture `readCarry` is written in and
// for the same reason: this is a convenience over state that was always
// disposable, and refusing to start over it would be a self-inflicted outage.
func (d *Daemon) readReopen() []string {
	raw, err := os.ReadFile(d.reopenPath())
	if err != nil {
		return nil
	}
	var rec reopenRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		log.Printf("[atrium] the record of what was open is unreadable, ignoring it: %v", err)
		return nil
	}
	return rec.Cards
}

// reopenSaved starts a runner again on every card that had one.
//
// MUST RUN AFTER `startFixtures`, and the ordering is load-bearing rather than
// tidy. A fixture card is on this list too, since it had a runner when the
// daemon stopped. Running first would start it here, and then the fixture
// would find it already up and leave it alone, which sounds harmless and is
// not: the fixture is what pins the card, themes it, and decides whether it
// resumes from the directory or from the card. Letting this path win would
// quietly change how a fixture starts.
//
// So fixtures go first and everything they started is skipped here.
func (d *Daemon) reopenSaved() {
	wanted := d.reopenWanted()
	if len(wanted) == 0 {
		return
	}

	log.Printf("[atrium] reopening %d terminal(s) that were open before the restart", len(wanted))
	reopened := 0
	for i, t := range wanted {
		if i > 0 {
			time.Sleep(reopenGap)
		}
		req := LaunchRequest{
			Harness: t.Runner,
			Cwd:     t.Worktree,
			TaskID:  t.ID,
			Resume:  d.reopenResume(t),
			// THE MODEL COMES BACK TOO, and this is the line the whole
			// durable side of that feature exists for.
			//
			// This rebuilds a launch out of the card, so anything not named
			// here reverts to the runner's default. A session started on one
			// model and reopened without it would move back on the next
			// restart, silently, which is the failure choosing a model was
			// meant to solve arriving through a different door.
			//
			// `Launch` falls back to the card's own value anyway, so this is
			// belt and braces. It is written out because the fallback is in
			// another file and a future edit there would take this with it
			// without anybody noticing.
			Model: t.Model,
		}
		if _, err := d.Launch(req); err != nil {
			// Logged and stepped over, one card at a time. A worktree that has
			// been deleted, a harness that has been disabled, or a claude that
			// is no longer on PATH must not stop the rest coming back.
			log.Printf("[atrium] could not reopen %s: %v", t.ID, err)
			continue
		}
		reopened++
	}
	log.Printf("[atrium] reopened %d of %d terminal(s)", reopened, len(wanted))
}

// reopenWanted is the filter, split out because it IS the decision.
//
// Whether claude starts is the operating system's business. Which cards are
// asked to start is this file's, and every clause below is a way of reopening
// the wrong thing.
func (d *Daemon) reopenWanted() []*store.Task {
	cards := d.readReopen()
	if len(cards) == 0 {
		return nil
	}

	var wanted []*store.Task
	for _, id := range cards {
		if d.sup.get(id) != nil {
			// A fixture already brought this one up, or a runner reconnected.
			continue
		}
		t, err := d.st.Get(id)
		if err != nil {
			// The card was pruned or archived while the daemon was down. Not
			// worth a warning: the work is gone, which is an answer.
			continue
		}
		if t.Runner == "" {
			log.Printf("[atrium] not reopening %s: nothing records which runner it used", id)
			continue
		}
		if t.Worktree == "" {
			log.Printf("[atrium] not reopening %s: no directory recorded", id)
			continue
		}
		// A card somebody put down is a standing no, and reopening it would
		// undo that by way of a restart. Shelving already refuses on the
		// agent's behalf in the permission chain, so a reopened runner would
		// sit blocked behind a card nobody is looking at.
		if t.Status == store.StatusShelved {
			continue
		}
		wanted = append(wanted, t)
	}
	return wanted
}

// reopenResume is the conversation to pick back up, checked before it is used.
//
// THE CARD'S OWN ID AND NOT THE LATEST IN THE DIRECTORY, which is where this
// differs from a fixture. A fixture without a resume mode means "carry on with
// whatever happened in that folder", because it is a standing terminal. This
// is a specific session that was open a minute ago, and the newest
// conversation in the directory may belong to a different card in the same
// worktree.
//
// A stale id makes the runner exit within a second saying so, which reaches
// the board as a dead card and a terminal that never appeared, so it is
// checked against what is on disk first and dropped when it is gone. Starting
// fresh in the right directory is worth more than failing to resume.
func (d *Daemon) reopenResume(t *store.Task) string {
	id := t.ResumeID
	if id == "" {
		return ""
	}
	if !api.SessionExists(t.Worktree, id) {
		log.Printf("[atrium] %s resumed conversation %s, which is gone. starting fresh", t.ID, id)
		return ""
	}
	return id
}
