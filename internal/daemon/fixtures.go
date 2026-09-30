package daemon

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

// Terminals that come up with the daemon.
//
// The habit this replaces: open a terminal, cd somewhere, resume the same
// agent, every morning. Some sessions are permanent, and starting them by hand
// is a ritual rather than a decision.
//
// Started in order, one at a time. Concurrently would be faster and would also
// mean the order somebody chose is only true until two of them race, which
// makes "the dotfiles one is always first" a lie.

// fixtureGap is the pause between starting one and the next.
//
// Not for correctness. Half a dozen runners launching in the same instant all
// resolve their harness, run their prepare command and open a pty at once, and
// on a cold machine that is a stampede somebody watches for ten seconds
// wondering which one failed.
const fixtureGap = 400 * time.Millisecond

// startFixtures brings up every enabled fixture.
//
// Runs in the background so a slow runner cannot delay the board coming up. A
// board that is not answering yet is worse than a terminal that is not open
// yet, because the second is visible and the first looks like a hang.
func (d *Daemon) startFixtures() {
	fixtures, err := d.st.Fixtures()
	if err != nil {
		log.Printf("[atrium] could not read the fixtures: %v", err)
		return
	}
	var wanted []*store.Fixture
	for _, f := range fixtures {
		if f.Enabled {
			wanted = append(wanted, f)
		}
	}
	if len(wanted) == 0 {
		return
	}

	// Named before the first one is started, and as a whole list. See
	// settling.go: the board stays quiet about arrivals until every one of
	// these has come back, which is the only definition of "still coming up"
	// that does not depend on guessing how long a cold claude takes.
	//
	// A fixture has no card until it starts one, so it is expected under its
	// own identity and reported the same way.
	ids := make([]string, 0, len(wanted))
	for _, f := range wanted {
		ids = append(ids, "fixture:"+f.ID)
	}
	d.settle.expect(ids)

	log.Printf("[atrium] starting %d fixture(s)", len(wanted))
	// One report for the batch, not one per fixture.
	//
	// Every fixture that comes up produces a card that is ready, and a ready
	// card is something the board announces. Half a dozen terminals opening at
	// boot was therefore half a dozen notifications for an event with no
	// content: you configured them to start, and they started.
	//
	// The board suppresses those on its own, because a card that is ready
	// BECAUSE IT JUST STARTED never rings whoever launched it. What the board
	// cannot work out for itself is the other half, the fixtures that were
	// supposed to produce a card and did not, since the absence of a card is
	// not an event. So the batch says so once, here, with the count of each.
	var failed []FixtureFault
	started := 0
	for i, f := range wanted {
		if i > 0 {
			time.Sleep(fixtureGap)
		}
		// A CARD SOMEBODY ENDED STAYS ENDED ACROSS A RESTART. An exit is a
		// human or a launcher saying "this is over", and a fixture bringing it
		// back at the next boot undid that: on 2026-09-30 the orchestrator
		// moved to another room, exited this card, and the 11:28 room deploy
		// started it again as a second copy of the conversation it had moved
		// to (r-new-reopen-resumes-exited-card). Starting it from the fixtures
		// page still works: that is somebody asking now.
		if why := d.fixtureEnded(f); why != "" {
			log.Printf("[atrium] fixture %q not started: %s", fixtureName(f), why)
			if err := d.st.NoteFixtureRun(f.ID, why); err != nil {
				log.Printf("[atrium] could not record why fixture %q did not start: %v", fixtureName(f), err)
			}
			d.settle.arrived("fixture:" + f.ID)
			continue
		}
		// Started or failed, this one is no longer something atrium is waiting
		// to come back. A fixture whose worktree has gone would otherwise hold
		// the window open to the backstop.
		err := d.startFixture(f)
		d.settle.arrived("fixture:" + f.ID)
		if err != nil {
			failed = append(failed, FixtureFault{Label: fixtureName(f), Reason: err.Error()})
			continue
		}
		started++
	}
	d.ap.Broadcast("fixtures-started", map[string]any{
		"started": started,
		"failed":  failed,
	})
}

// FixtureFault is one fixture that did not start, and why.
type FixtureFault struct {
	Label  string `json:"label"`
	Reason string `json:"reason"`
}

// fixtureEnded is why a fixture must not start at boot, or empty: somebody
// asked its card to exit and nothing launched it since. Never the card's
// status, which a wind-down moves to `done` for every card. See store.ExitAsked.
func (d *Daemon) fixtureEnded(f *store.Fixture) string {
	if f.TaskID == "" {
		return ""
	}
	if asked, err := d.st.ExitAsked(f.TaskID); err != nil || !asked {
		return ""
	}
	return "its card was asked to exit, so a restart leaves it down. start it here to bring it back"
}

// fixtureName is what to call one in a message. The label, or the directory
// when it has none, which is the same fallback a card's title uses.
func fixtureName(f *store.Fixture) string {
	if f.Label != "" {
		return f.Label
	}
	return f.Cwd
}

// startFixture brings up one, and remembers the card it landed on.
//
// The error is returned AND recorded on the row, but the caller keeps going:
// one fixture pointing at a directory that no longer exists must not stop the
// rest, and it must certainly not stop the daemon. It is returned so the batch
// can count, and recorded so the page listing fixtures is also the page that
// answers why one is missing.
//
// A fixture that was already running is neither a start nor a failure. Nothing
// was asked of it, so nothing is written about it.
func (d *Daemon) startFixture(f *store.Fixture) error {
	name := fixtureName(f)

	// Onto the card it used last time, so the same conversation continues
	// rather than a second card appearing beside it every morning.
	onto := f.TaskID
	if onto == "" {
		// First run. A fixture pointed at a directory that already has a live
		// card takes that one over: two cards for one directory, with the same
		// name, is the exact confusion this is meant to prevent, and it would
		// happen to anybody who pins a session and then writes a fixture for
		// it.
		if adopted, err := d.st.AdoptableTask(f.Cwd); err == nil && adopted != "" {
			onto = adopted
			log.Printf("[atrium] fixture %q adopted the card already in %s", name, f.Cwd)
		}
	}

	// Already up. A fixture is "make sure this terminal exists", not "open
	// another one", and starting a second is worse than doing nothing twice
	// over: two sessions in one directory both write to the same files, and
	// the conversation the fixture was meant to continue is now open in the
	// one it is not attached to.
	if onto != "" && d.sup.get(onto) != nil {
		log.Printf("[atrium] fixture %q is already running, leaving it alone", name)
		return nil
	}

	req := LaunchRequest{
		Harness: f.Harness,
		Cwd:     f.Cwd,
		Title:   f.Label,
		TaskID:  onto,
	}
	var held *heldResume
	if f.Resume {
		req.Resume, held = d.fixtureResume(f, onto)
	}

	task, err := d.Launch(req)
	if err != nil {
		log.Printf("[atrium] fixture %q did not start: %v", name, err)
		if noteErr := d.st.NoteFixtureRun(f.ID, err.Error()); noteErr != nil {
			log.Printf("[atrium] could not record the failure for fixture %q: %v", name, noteErr)
		}
		return err
	}
	log.Printf("[atrium] fixture %q started as %s", name, task.ID)
	// On the card it made, now that there is one.
	d.noteHeldResume(task.ID, "fixture", held)
	// Cleared on success, so a fixture that has been fixed stops reporting the
	// thing that used to be wrong with it.
	if err := d.st.NoteFixtureRun(f.ID, ""); err != nil {
		log.Printf("[atrium] could not record the start of fixture %q: %v", name, err)
	}

	if task.ID != f.TaskID {
		if err := d.st.NoteFixtureTask(f.ID, task.ID); err != nil {
			log.Printf("[atrium] could not remember the card for fixture %q: %v", name, err)
		}
	}
	// A fixture is by definition something worth keeping in front of you, so
	// its card is pinned without being asked.
	if err := d.st.SetPinned(task.ID, true); err != nil {
		log.Printf("[atrium] could not pin fixture %q: %v", name, err)
	}
	if t := strings.TrimSpace(f.Theme); t != "" {
		if err := d.st.SetTheme(task.ID, t); err != nil {
			log.Printf("[atrium] could not theme fixture %q: %v", name, err)
		}
	}
	return nil
}

// StartFixtureNow brings one up on demand, so a definition can be tried
// without restarting the daemon to find out whether it works.
//
// The failure is returned here rather than swallowed. This is one fixture
// somebody just pressed start on, so there is a person waiting to hear, which
// is the opposite of the boot case.
func (d *Daemon) StartFixtureNow(id string) error {
	f, err := d.st.GetFixture(id)
	if err != nil {
		return err
	}
	return d.startFixture(f)
}

// fixtureResume is WHICH conversation this fixture picks back up.
//
// The mode exists because the boolean answered the wrong question. `resume` on
// meant "the id recorded on the card I started last time", and a fixture does
// not want a particular conversation, it wants the terminal it had. That id
// goes stale the moment anything else starts a session in the directory, and
// points at nothing once the transcript is deleted, and both fail the same
// silent way: a fresh conversation, no error, repeated every restart.
//
// Anything that does not exist on disk is dropped rather than passed on, and
// said out loud. Handing a runner an id it cannot find is how this was
// invisible: the runner starts fresh and reports nothing wrong.
func (d *Daemon) fixtureResume(f *store.Fixture, onto string) (string, *heldResume) {
	cwd := strings.TrimSpace(f.Cwd)
	var held *heldResume

	if strings.EqualFold(strings.TrimSpace(f.ResumeMode), "card") {
		id := d.resumeIDFor(onto)
		if id != "" && cwd != "" && !api.SessionExists(cwd, id) {
			log.Printf("[atrium] fixture %q resumes card %s, whose conversation %s is gone. "+
				"starting fresh", fixtureName(f), onto, id)
			return "", nil
		}
		return id, nil
	}

	// The default, and what `resume` on has always meant to a person. But not
	// a conversation another live card is running: the newest in a shared
	// checkout is often somebody else's.
	if cwd != "" {
		if id := api.LatestSession(cwd); id != "" {
			h := d.resumeHeld(onto, id)
			if h == nil {
				return id, nil
			}
			held = &heldResume{resume: id, holder: h}
		}
	}
	// Nothing on disk to go back to. The card's own id is the last thing worth
	// trying, and it is checked like everything else.
	id := d.resumeIDFor(onto)
	if id != "" && cwd != "" && !api.SessionExists(cwd, id) {
		return "", held
	}
	if h := d.resumeHeld(onto, id); h != nil {
		return "", &heldResume{resume: id, holder: h}
	}
	return id, held
}

// resumeHeld is whether a conversation belongs to a live card other than
// `taskID`. Resuming it would open one conversation in two terminals, both
// writing to it. Said out loud, because the runner would start fresh without a
// word and nobody would know why.
//
// THIS ROOM ONLY. A card on another room on the same machine can hold it too,
// and nothing here can see that room's cards. What closes that path is the
// boot rule above: a fixture whose card was ended is not started.
func (d *Daemon) resumeHeld(taskID, resumeID string) *store.Task {
	if resumeID == "" {
		return nil
	}
	holders, err := d.st.ResumeHolders(resumeID, taskID)
	if err != nil {
		return nil
	}
	for _, h := range holders {
		// A holder coming back in this same boot pass is live too, though it has
		// no runner yet. See reopenSaved.
		first, _ := d.bootResumes.Load(resumeID)
		if !d.runnerIsLive(h) && first != h.ID {
			continue
		}
		log.Printf("[atrium] not resuming conversation %s onto %s: %s (%s) holds it and is live",
			resumeID, orWord(taskID, "a new card"), h.DisplayTitle(), h.ID)
		return h
	}
	return nil
}

// heldResume is a resume refused because a live card holds the conversation,
// kept until there is a card to say so on.
type heldResume struct {
	resume string
	holder *store.Task
}

// noteHeldResume puts a refused resume on the card that started fresh, where
// somebody looks, and not only in the log. `via` is who refused it. Written
// only once the card exists: a fixture's first start has none until it
// launches, and an event on no card is refused by the store.
func (d *Daemon) noteHeldResume(taskID, via string, h *heldResume) {
	if h == nil || taskID == "" {
		return
	}
	d.noteResume(taskID, store.ResumeRefused, map[string]any{
		"resume": h.resume, "holder": h.holder.ID, "via": via,
		"reason": fmt.Sprintf("started fresh rather than resume conversation %s: %q holds it and is live",
			h.resume, h.holder.DisplayTitle()),
	})
}

// resumeIDFor is the conversation to pick back up, when there is one.
//
// Empty for a card atrium has never started, or one whose runner never
// reported a resume id, and an empty resume is a fresh start rather than an
// error.
func (d *Daemon) resumeIDFor(taskID string) string {
	if taskID == "" {
		return ""
	}
	t, err := d.st.Get(taskID)
	if err != nil {
		return ""
	}
	return t.ResumeID
}
