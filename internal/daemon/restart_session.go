package daemon

import (
	"fmt"
	"log"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Restarting one session, as opposed to restarting the whole daemon.
//
// It is `Unshelve` without the shelve: the card never moves, and its history and
// resume id are the reason the new process is the old conversation continued
// rather than a fresh start. Restart is for a session that is wedged, out of
// context, or running a binary that has since been rebuilt, where the fix is a
// clean process on the same thread.

// restartGoneWait bounds how long to wait for the old runner to be gone before
// starting the new one.
//
// StopRunner blocks through the wind-down, but the supervisor entry is removed a
// beat later on the runner's own goroutine, and `Launch` onto this card refuses
// while one is still registered. So the wait is for the slot to clear, not for
// the process, which windDown already saw out.
const restartGoneWait = 8 * time.Second

// RestartRunner asks a card's runner to exit, waits for it to be gone, and
// starts the same conversation again on the SAME card.
func (d *Daemon) RestartRunner(taskID string) (*store.Task, error) {
	t, err := d.st.Get(taskID)
	if err != nil {
		return nil, err
	}
	// Hold the card's launch lock across the whole stop-then-start, and around the
	// ownership check itself. Between stopping the old process and registering the
	// new one there is a window in which this card owns no runner, and a second
	// restart or a Launch onto this card would pass its liveness guard and spawn a
	// second process resuming the same conversation. That is the braided
	// transcript. The resume id is locked too, so a launch resuming the same
	// conversation onto a different card cannot slip through either. Launch takes
	// the same lock, so the two paths serialize. See keyedmutex.go and launch.go's
	// resumeIsFree and ontoRefusal.
	//
	// Keyed on the card's current resume id. It is the conversation id the runner
	// records and it is stable across a restart, so it matches the id the launch
	// below actually resumes even though that is re-read after the exit.
	//
	// The `sup.get` check is INSIDE the lock: a concurrent restart that already
	// holds it may be mid stop-then-start, when this card momentarily owns no
	// runner, and checking before the lock would read that gap as "nothing to
	// restart" and refuse a card that is about to have a runner again.
	unlock := d.launching.lock(launchKeys(taskID, t.ResumeID)...)
	defer unlock()

	// Only a session atrium owns can be restarted from here. A window-mode
	// launch owns itself and a session that joined by hand belongs to whoever
	// started it, so there is no terminal here to exit and nothing to relaunch.
	if d.sup.get(taskID) == nil {
		return nil, fmt.Errorf("atrium does not own a terminal for %s, so there is nothing "+
			"here to restart. it may be a window-mode session, which owns itself", t.DisplayTitle())
	}
	if t.Runner == "" {
		return nil, fmt.Errorf("atrium does not know which runner %s used, so it cannot "+
			"be started again", t.DisplayTitle())
	}
	if t.Worktree == "" {
		return nil, fmt.Errorf("%s has no directory recorded, so it cannot be started again",
			t.DisplayTitle())
	}

	// Ask it to exit the way its harness says to, and wait for the slot to clear.
	if err := d.StopRunner(taskID); err != nil {
		return nil, err
	}
	if !d.waitRunnerGone(taskID, restartGoneWait) {
		return nil, fmt.Errorf("%s did not stop in time, so it was not restarted. try again, "+
			"or terminate it and start it from the card", t.DisplayTitle())
	}

	// Re-read AFTER the exit, because a session records its resume id on the way
	// out. The freshest id is the conversation to pick up, and using the stale
	// one would resume a step behind where the session actually left off.
	fresh, err := d.st.Get(taskID)
	if err != nil {
		return nil, err
	}

	// The card is the launch spec, exactly as reopenSaved and Unshelve build it:
	// same harness, same directory, same conversation, same model. reopenResume
	// checks the id against what is on disk and drops it when the conversation is
	// gone, so a session whose history was cleared starts fresh in the right
	// place rather than dying on a stale resume.
	req := LaunchRequest{
		Harness: fresh.Runner,
		Cwd:     fresh.Worktree,
		TaskID:  taskID,
		Resume:  d.reopenResume(fresh),
		Model:   fresh.Model,
	}
	// launchLocked, not Launch: this call already holds the launch lock for this
	// card and resume, and Launch would try to take it again.
	started, err := d.launchLocked(req)
	if err != nil {
		return nil, fmt.Errorf("could not start %s again: %w", fresh.DisplayTitle(), err)
	}
	log.Printf("[atrium] %s restarted", fresh.DisplayTitle())
	return started, nil
}

// waitRunnerGone blocks until no runner is registered for a card, or the
// deadline passes. See restartGoneWait for why the wait exists at all.
func (d *Daemon) waitRunnerGone(taskID string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for {
		if d.sup.get(taskID) == nil {
			return true
		}
		if time.Now().After(deadline) {
			return d.sup.get(taskID) == nil
		}
		time.Sleep(50 * time.Millisecond)
	}
}
