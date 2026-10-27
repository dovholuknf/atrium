package daemon

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

// TERMINALS THAT WERE OPEN COME BACK AFTER A RESTART.
//
// Before this, only fixtures did. A fixture is a standing decision, "this
// terminal exists every day", and everything else was left behind: a card
// launched from a pull request, an resumed piece of work, anything started
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

// reopenWorkers is how many cards are started at once. A fixed pause between cards cost 400ms each, ten
// seconds for twenty-seven, so the stampede is bounded by a few workers instead.
const reopenWorkers = 4

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

// saveReopen adds the cards that had a runner atrium owned to the list.
//
// Called from the wind-down beside `saveCarryover`, and best effort in the
// same way: every failure is logged and stepped over. A shutdown must not fail
// or hang over this, and the cost of it not being written is the behaviour
// that existed before it did.
//
// ADDED TO, NEVER REPLACED. A card a reboot or a pty host crash killed is on the list and has no runner now, and
// replacing the list left it dead. A card closed on purpose drops off through `openCards`.
func (d *Daemon) saveReopen(rs []*runner) {
	d.reopenMu.Lock()
	defer d.reopenMu.Unlock()
	cards := d.readReopen()
	for _, r := range rs {
		if r != nil && r.taskID != "" {
			cards = append(cards, r.taskID)
		}
	}
	if n, ok := d.writeReopen(cards); ok {
		log.Printf("[atrium] recorded %d open terminal(s) to reopen", n)
	}
}

// noteOpen puts a card on the list the moment its runner starts. A reboot, an OOM or a crash gives the wind-down no
// chance to write, and a list written only there named the cards open at the last clean stop, a week old on
// 2026-10-27.
func (d *Daemon) noteOpen(taskID string) {
	d.reopenMu.Lock()
	defer d.reopenMu.Unlock()
	cards := d.readReopen()
	if slices.Contains(cards, taskID) {
		return
	}
	d.writeReopen(append(cards, taskID))
}

// openCards is the list without repeats and without the cards that are closed for good: pruned, archived, or
// exited on purpose. Everything else stays, since a card a failure killed is the one this list exists for.
func (d *Daemon) openCards(cards []string) []string {
	var kept []string
	for _, id := range cards {
		if slices.Contains(kept, id) {
			continue
		}
		t, err := d.st.Get(id)
		if err != nil || t.ArchivedAt != nil {
			continue
		}
		if asked, err := d.st.ExitAsked(id); err == nil && asked {
			continue
		}
		kept = append(kept, id)
	}
	return kept
}

// writeReopen writes the list through `openCards` and returns how many it kept. Callers hold reopenMu.
func (d *Daemon) writeReopen(cards []string) (int, bool) {
	rec := reopenRecord{SavedAt: time.Now(), Cards: d.openCards(cards)}
	raw, err := json.Marshal(rec)
	if err != nil {
		log.Printf("[atrium] could not record what was open: %v", err)
		return 0, false
	}
	path := d.reopenPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("[atrium] could not make a place to record what was open: %v", err)
		return 0, false
	}
	// Written aside and renamed, so a daemon killed mid-write leaves the last
	// good list rather than half of this one.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		log.Printf("[atrium] could not record what was open: %v", err)
		return 0, false
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		log.Printf("[atrium] could not record what was open: %v", err)
		return 0, false
	}
	return len(rec.Cards), true
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

	// The whole list, before the first one starts. See settling.go: these are
	// the cards the board must not announce as new, and there is no interval
	// that reliably covers ten claude sessions resuming on a cold machine.
	ids := make([]string, 0, len(wanted))
	for _, t := range wanted {
		ids = append(ids, t.ID)
	}
	d.settle.expect(ids)

	// ONE CONVERSATION, ONE CARD, OVER THE WHOLE LIST. Two cards on the list with
	// one resume id would each find the other not yet running and both resume it.
	// The first on the list takes it, and the rest start fresh (resumeHeld).
	for _, t := range wanted {
		if t.ResumeID != "" {
			d.bootResumes.LoadOrStore(t.ResumeID, t.ID)
		}
	}
	defer d.bootResumes.Clear()

	log.Printf("[atrium] reopening %d terminal(s) that were open before the restart", len(wanted))
	reopened := reopenEach(wanted, func(t *store.Task) error {
		// The card is the launch spec, model and extras included. See reopenRequest.
		_, err := d.Launch(d.reopenRequest(t))
		// Back, or never coming. Both end the wait on this card: a worktree
		// that has gone would otherwise keep the board quiet until the
		// backstop.
		d.settle.arrived(t.ID)
		if err != nil {
			// Logged and stepped over, one card at a time. A worktree that has
			// been deleted, a harness that has been disabled, or a claude that
			// is no longer on PATH must not stop the rest coming back.
			log.Printf("[atrium] could not reopen %s: %v", t.ID, err)
		}
		return err
	})
	log.Printf("[atrium] reopened %d of %d terminal(s)", reopened, len(wanted))
}

// reopenEach runs start over every card with at most reopenWorkers in flight, in list order, and returns how many
// succeeded.
func reopenEach(wanted []*store.Task, start func(*store.Task) error) int {
	var ok atomic.Int32
	next := make(chan *store.Task)
	var wg sync.WaitGroup
	for i := 0; i < reopenWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range next {
				if start(t) == nil {
					ok.Add(1)
				}
			}
		}()
	}
	for _, t := range wanted {
		next <- t
	}
	close(next)
	wg.Wait()
	return int(ok.Load())
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
		if err != nil || t.ArchivedAt != nil {
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
		if t.Status == store.StatusPaused {
			continue
		}
		// AN ASKED EXIT IS KEPT. Not `done`: a wind-down ends every session and
		// each lands in `done`, which is what this list exists to bring back.
		if asked, err := d.st.ExitAsked(t.ID); err == nil && asked {
			continue
		}
		// Parked is put down the same way: it wakes when something asks it to.
		if isParked(t) {
			continue
		}
		// Do not reopen throwaways. Their directories may be gone or awaiting
		// startup cleanup, so directory existence alone does not make reopening safe.
		if t.Throwaway {
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
	if h := d.resumeHeld(t.ID, id); h != nil {
		d.noteHeldResume(t.ID, "reopen", &heldResume{resume: id, holder: h})
		return ""
	}
	return id
}
