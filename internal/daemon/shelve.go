package daemon

import (
	"fmt"
	"log"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Shelving stops the session. Unshelving starts the same conversation again.
//
// Before this, shelving moved a card and left the runner sitting there holding
// a conversation open, which made shelving expensive enough to avoid. The point
// of shelving is to put work down, and work that is still running is not down.
//
// What makes it safe is the resume id. A session's own id for its conversation
// is recorded on every session event, so stopping the process loses the
// terminal and not the thread. See store.SetResumeID.

// shelveGrace is how long a runner gets to wind up when its card is shelved.
//
// Shorter than shutdown's ten seconds, because shutdown is stopping everything
// at once and this is one card the operator is putting down while watching.
const shelveGrace = 5 * time.Second

// StopRunner asks a runner to exit the way its harness says to, without
// shelving the card.
//
// The alternative was attaching, typing whatever that runner wants, and waiting.
// Terminate exists for when that does not work: it kills the process outright.
// This one asks.
func (d *Daemon) StopRunner(taskID string) error {
	t, err := d.st.Get(taskID)
	if err != nil {
		return err
	}
	if !d.stopOne(taskID, shelveGrace) {
		return fmt.Errorf("atrium does not own a terminal for %s, so there is "+
			"nothing here to exit", t.DisplayTitle())
	}
	// awaitExit records the exit and moves the card, so nothing to do here but
	// say it happened.
	log.Printf("[atrium] %s asked to exit", t.DisplayTitle())
	return nil
}

// Shelve puts a card down and stops its runner.
//
// The card, its history and its resume id all stay. Requests already waiting
// are answered by the caller before this runs, since a shelved card is a
// standing no and leaving a question pending would freeze the agent behind a
// card nobody is looking at.
func (d *Daemon) Shelve(taskID string) error {
	t, err := d.st.Get(taskID)
	if err != nil {
		return err
	}

	stopped := d.stopOne(taskID, shelveGrace)
	if stopped {
		if err := d.st.AppendEvent(taskID, store.EventExited, map[string]any{
			"by": "shelved", "resume": t.ResumeID,
		}); err != nil {
			return err
		}
		d.act.forget(taskID)
		log.Printf("[atrium] %s shelved, its runner stopped", t.DisplayTitle())
		return nil
	}

	// Not supervised, so there is no terminal to close. A window-mode launch
	// owns itself and a session that joined by hand belongs to whoever started
	// it, and killing either from here would be reaching into a process atrium
	// was never given.
	if t.PID > 0 {
		log.Printf("[atrium] %s shelved. atrium does not own its process, so it keeps running",
			t.DisplayTitle())
	}
	return nil
}

// Unshelve picks a shelved card back up, starting its runner again from where
// the conversation left off.
//
// Returns the reason when it cannot, so the caller can move the card anyway and
// say why nothing started. Failing to relaunch must not leave the card stuck in
// shelved: the operator asked for it back.
func (d *Daemon) Unshelve(taskID string) (started bool, why string, err error) {
	t, err := d.st.Get(taskID)
	if err != nil {
		return false, "", err
	}
	if d.sup.get(taskID) != nil {
		return false, "it is already running", nil
	}
	if t.ResumeID == "" {
		return false, "atrium never learned this session's resume id, so there is " +
			"nothing to pick up. start a new one from the card", nil
	}
	if t.Runner == "" {
		return false, "atrium does not know which runner this was", nil
	}

	// The card is the launch spec: runner is the harness, worktree is where it
	// ran, resume id is the conversation.
	if _, err := d.Launch(LaunchRequest{
		Harness: t.Runner,
		Cwd:     t.Worktree,
		Resume:  t.ResumeID,
		TaskID:  taskID,
	}); err != nil {
		return false, fmt.Sprintf("could not start it again: %v", err), nil
	}
	log.Printf("[atrium] %s unshelved and resumed", t.DisplayTitle())
	return true, "", nil
}
