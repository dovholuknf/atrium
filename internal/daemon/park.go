package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/dovholuknf/atrium/internal/store"
)

// Parking, and the human touch that decides what is worth keeping up. See
// docs/rnd/keepalive-policy-design.md sections 1, 4, 5 and 7.
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
	// A card that took a handoff is told to read it, and this is queued BEFORE the
	// caller queues whatever woke it, so the message follows the wake prompt.
	d.queueHandoffWake(taskID)
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

// attachParked is an attach to a card with no process. The socket is accepted so
// the board can show it, and NOTHING RESUMES until a real key arrives: focus
// reports, clicks and terminal answers are not keys, which is what
// `typedLine.feed` decides, and a card opened to look at must stay asleep.
//
// The first key resumes it. That frame is held, written to the runner once it is
// up, and the socket closes as a restart so the board reattaches to the runner
// the ordinary way rather than this function growing a second copy of attach.
func (d *Daemon) attachParked(w http.ResponseWriter, r *http.Request, t *store.Task) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		log.Printf("[atrium] attach parked %s: %v", t.ID, err)
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(4 << 20)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()

	_ = c.Write(ctx, websocket.MessageBinary, []byte("\r\n\x1b[38;5;244m[atrium] "+t.DisplayTitle()+
		" is parked: idle, no process. press any key to resume it.\x1b[0m\r\n"))

	var line typedLine
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		var in attachIn
		if json.Unmarshal(data, &in) != nil || in.T != "in" || !line.feed([]byte(in.D)) {
			continue
		}
		_ = c.Write(ctx, websocket.MessageBinary, []byte("\x1b[38;5;244m[atrium] resuming...\x1b[0m\r\n"))
		if err := d.unpark(t.ID, ViaTyped); err != nil {
			_ = c.Write(ctx, websocket.MessageBinary, []byte("\x1b[31m[atrium] "+err.Error()+"\x1b[0m\r\n"))
			continue
		}
		if run := d.sup.get(t.ID); run != nil {
			_ = run.writeOperatorInput([]byte(in.D))
		}
		c.Close(websocket.StatusNormalClosure, "restarting")
		return
	}
}

// handleResume is `POST /v1/tasks/{id}/resume`: the board's Resume entry. A card
// that is not parked answers ok and does nothing, so a double press is harmless.
func (d *Daemon) handleResume(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := d.st.Get(id); err != nil {
		writeJSONErr(w, http.StatusNotFound, err)
		return
	}
	if err := d.unpark(id, ViaResume); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// fromFamily is whether a say's sender is the target's launcher or one of the
// target's own workers (work_item.launcher_id, so a report_to child counts too).
// Such a sender is the one the parked card is waiting on or working for, so its
// say resumes the card as if wake=true. Anyone else still gets `parked`.
func (d *Daemon) fromFamily(from string, target *store.Task) bool {
	if from == "" || target == nil {
		return false
	}
	sender, err := d.st.GetByWireName(from)
	if err != nil {
		if sender, err = d.st.GetByWireName(d.st.Qualify(from)); err != nil {
			if sender, err = d.st.GetByAlias(from); err != nil {
				return false
			}
		}
	}
	if w, err := d.st.WorkItem(target.ID); err == nil && w.LauncherID == sender.ID {
		return true
	}
	if w, err := d.st.WorkItem(sender.ID); err == nil && w.LauncherID == target.ID {
		return true
	}
	// A card launched with lineage but no work item yet is family the same way.
	if l := d.launcherOf(target); l != nil && l.ID == sender.ID {
		return true
	}
	if l := d.launcherOf(sender); l != nil && l.ID == target.ID {
		return true
	}
	return false
}

// wakeVia is what woke a parked card by a say: the operator's own channel has no
// sender, a session does.
func wakeVia(from string) string {
	if from == "" {
		return ViaMessage
	}
	return "say"
}

// parkedNote is what the sender of a say to a parked card is told, with nothing
// queued.
func parkedNote(t *store.Task) string {
	since := ""
	if t.ParkedAt != nil {
		since = " since " + t.ParkedAt.Local().Format("15:04")
	}
	return fmt.Sprintf("not sent: this card is parked, send again with wake=true to resume it. %s is idle%s "+
		"with no process, and its cache is probably cold, so waking it runs a turn on a full context.",
		t.WireName, since)
}
