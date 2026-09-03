package daemon

import (
	"log"
	"strings"
	"time"

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

	log.Printf("[atrium] starting %d fixture(s)", len(wanted))
	for i, f := range wanted {
		if i > 0 {
			time.Sleep(fixtureGap)
		}
		d.startFixture(f)
	}
}

// startFixture brings up one, and remembers the card it landed on.
//
// A failure is logged and skipped rather than returned. One fixture pointing
// at a directory that no longer exists must not stop the rest, and it must
// certainly not stop the daemon.
func (d *Daemon) startFixture(f *store.Fixture) {
	name := f.Label
	if name == "" {
		name = f.Cwd
	}

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

	req := LaunchRequest{
		Harness: f.Harness,
		Cwd:     f.Cwd,
		Title:   f.Label,
		TaskID:  onto,
	}
	if f.Resume {
		req.Resume = d.resumeIDFor(onto)
	}

	task, err := d.Launch(req)
	if err != nil {
		log.Printf("[atrium] fixture %q did not start: %v", name, err)
		return
	}
	log.Printf("[atrium] fixture %q started as %s", name, task.ID)

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
}

// StartFixtureNow brings one up on demand, so a definition can be tried
// without restarting the daemon to find out whether it works.
func (d *Daemon) StartFixtureNow(id string) error {
	f, err := d.st.GetFixture(id)
	if err != nil {
		return err
	}
	d.startFixture(f)
	return nil
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
