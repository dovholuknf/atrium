package daemon

import (
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Parking, and the human touch that decides what is worth keeping up. See
// docs/keepalive-policy-design.md sections 1, 4, 5 and 7.
//
// A PARKED CARD IS A FLAG, NOT A STATUS. `parked_at` is set, the status it had
// is kept, its resume id is kept and no process runs. It sits in its column and
// wakes by `unpark`, which is the one function everything that resumes goes
// through.

// The vias a human touch can carry.
const (
	ViaTyped      = "typed"
	ViaPrompt     = "prompt"
	ViaPermission = "permission"
	ViaMessage    = "message"
	ViaAction     = "action"
	ViaEnable     = "enable"
	ViaResume     = "resume"
)

// humanTouchEvery is how often one card's touch is written. A person typing
// costs one store write a minute, against windows measured in hours.
const humanTouchEvery = time.Minute

type touchMark struct {
	at  time.Time
	via string
}

// touchWrites counts the store writes humanTouch has made, for the throttle's
// test to read.
var touchWrites atomic.Int64

// humanTouch records that a person just did something to a card.
//
// ON THE KEYSTROKE PATH, so a call inside the minute for the same via returns
// after one map lookup. The write happens off the calling path and every failure
// is swallowed, the hook posture: a keystroke must never fail because a
// timestamp could not be saved. It does NOT publish the card, since the board
// does not draw the touch on the face and a keystroke must not cost a frame.
//
// Called from the places design section 1 names and nowhere else. Each of them
// already had to tell a person from a machine for another reason.
func (d *Daemon) humanTouch(taskID, via string) {
	if taskID == "" {
		return
	}
	now := time.Now()
	if v, ok := d.humanTouched.Load(taskID); ok {
		if m := v.(touchMark); m.via == via && now.Sub(m.at) < humanTouchEvery {
			return
		}
	}
	d.humanTouched.Store(taskID, touchMark{at: now, via: via})
	touchWrites.Add(1)
	go func() {
		if err := d.st.TouchHuman(taskID, via, now); err != nil {
			log.Printf("[atrium] could not record a human touch on %s: %v", taskID, err)
		}
	}()
}

// isParked reports whether a card is parked.
func isParked(t *store.Task) bool { return t != nil && t.ParkedAt != nil }

// parkCard marks a card parked, with the status it had put back. One
// `status-changed` event says so. Published once, with no toast: the board draws
// a mark on the card.
func (d *Daemon) parkCard(taskID, was string, extra map[string]any) error {
	parked, err := d.st.Park(taskID, was, extra)
	if err != nil {
		return err
	}
	if parked {
		d.act.forget(taskID)
		d.publishTask(taskID)
	}
	return nil
}

// reopenRequest is the launch a card describes: the same harness, directory,
// conversation, model and extras. Shared by a reopen after a restart, a restart
// of one runner and an unpark, so the three cannot drift.
//
// THE MODEL COMES BACK TOO. This rebuilds a launch out of the card, so anything
// not named here reverts to the runner's default, and a session started on one
// model would move back on the next resume.
func (d *Daemon) reopenRequest(t *store.Task) LaunchRequest {
	return LaunchRequest{
		Harness: t.Runner,
		Cwd:     t.Worktree,
		TaskID:  t.ID,
		Resume:  d.reopenResume(t),
		Model:   t.Model,
		Effort:  t.Effort,
		Args:    t.LaunchArgs,
		Env:     t.LaunchEnv,
	}
}

// unpark brings a parked card back: the launch lock, the same launch a reopen
// makes, the flag cleared and one event. Idempotent: two triggers at once launch
// one runner, because the second finds the card no longer parked.
//
// `via` is what woke it. A human's cause stamps the human touch too, which puts
// the card back inside the keep-alive window. A peer's say and a report do not,
// since neither is a person.
//
// NEVER TYPES ANYTHING. A message that woke the card is queued by the caller
// through the ordinary path, after this returns. See peers.go for why.
func (d *Daemon) unpark(taskID, via string) error {
	t, err := d.st.Get(taskID)
	if err != nil {
		return err
	}
	unlock := d.launching.lock(launchKeys(taskID, t.ResumeID)...)
	defer unlock()

	// Read again inside the lock: whoever held it first has done the work.
	t, err = d.st.Get(taskID)
	if err != nil {
		return err
	}
	if !isParked(t) {
		return nil
	}
	if d.sup.get(taskID) == nil {
		if t.Runner == "" || t.Worktree == "" {
			return fmt.Errorf("%s cannot be resumed: nothing records which runner or directory it used",
				t.DisplayTitle())
		}
		if _, err := d.launchLocked(d.reopenRequest(t)); err != nil {
			return fmt.Errorf("could not resume %s: %w", t.DisplayTitle(), err)
		}
		d.startedAt.Store(taskID, time.Now())
	}
	if _, err := d.st.Unpark(taskID, via); err != nil {
		return err
	}
	if via == ViaResume || via == ViaMessage || via == ViaAction || via == ViaTyped {
		d.humanTouch(taskID, ViaResume)
	}
	log.Printf("[atrium] %s was parked and is resumed (%s)", t.DisplayTitle(), via)
	d.publishTask(taskID)
	return nil
}

// Answers a say to a card gets when it cannot simply be delivered.
const (
	sayOK     = ""
	sayGone   = "gone"
	sayParked = "parked"
)

// sayGate is the one question both say paths ask before delivering: is there
// somebody to read it. PARKED IS CHECKED BEFORE GONE, because a parked `done`
// card would otherwise read as ended, and the two answers differ in their last
// step: gone needs the sender's own hands, parked can be woken by asking.
func (d *Daemon) sayGate(t *store.Task) string {
	if isParked(t) {
		return sayParked
	}
	if d.sessionGone(t) {
		return sayGone
	}
	return sayOK
}

// parkedNote is what the sender of a say to a parked card is told, with nothing
// queued.
func parkedNote(t *store.Task) string {
	since := ""
	if t.ParkedAt != nil {
		since = " since " + t.ParkedAt.Local().Format("15:04")
	}
	return fmt.Sprintf("not sent: %s is parked (idle%s, no process). Its cache is probably cold, so waking it "+
		"runs a turn on a full context. Say it again with wake=true to resume it and deliver this.",
		t.WireName, since)
}
