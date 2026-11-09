package daemon

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/dovholuknf/atrium/internal/base/cardproc"
	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
)

// MoveInReason is what the held request is answered with when its session is moved into atrium.
const MoveInReason = "moved into atrium: this session is ending here and resuming under atrium's own terminal"

// moveIn is how `handleMoveIn` reaches the process. Zero fields take the real ones.
type moveIn struct {
	startTime func(pid int) (string, error)
	stop      func(pid int) error
	poll      time.Duration
	quiet     time.Duration
	limit     time.Duration
	resume    http.HandlerFunc
}

func (d *Daemon) moveInSeams() moveIn {
	m := d.mv
	if m.startTime == nil {
		m.startTime = cardproc.StartTime
	}
	if m.stop == nil {
		m.stop = cardproc.StopTree
	}
	if m.poll <= 0 {
		m.poll = 100 * time.Millisecond
	}
	if m.quiet <= 0 {
		m.quiet = 2 * time.Second
	}
	if m.limit <= 0 {
		m.limit = 20 * time.Second
	}
	if m.resume == nil {
		m.resume = d.handleResume
	}
	return m
}

// moveInRunner is the pid to end, or why this card cannot be moved in. It is the joined runner only while atrium holds
// no terminal for the card, the card is live and asking, its pid is the one on file and that process is still there.
func (d *Daemon) moveInRunner(t *store.Task, m moveIn) (pid int, started string, err error) {
	if d.sup.Get(t.ID) != nil {
		return 0, "", fmt.Errorf("%s already runs in a terminal atrium holds", t.DisplayTitle())
	}
	if small.IsParked(t) || (t.Status != store.StatusRunning && t.Status != store.StatusNeedsInput &&
		t.Status != store.StatusNeedsPermission) {
		return 0, "", fmt.Errorf("%s is not running, so there is nothing to move: resume it instead", t.DisplayTitle())
	}
	if t.Runner == "" || t.Worktree == "" {
		return 0, "", fmt.Errorf("%s cannot be moved in: nothing records which runner or directory it used",
			t.DisplayTitle())
	}
	if t.PID <= 0 {
		return 0, "", fmt.Errorf("%s has reported no process id, so atrium cannot tell which process to end",
			t.DisplayTitle())
	}
	started, err = m.startTime(t.PID)
	if errors.Is(err, cardproc.ErrGone) {
		return t.PID, "", nil
	}
	if err != nil {
		return 0, "", fmt.Errorf("could not check process %d: %w", t.PID, err)
	}
	return t.PID, started, nil
}

// handleMoveIn is `POST /v1/tasks/{id}/move-in`: a session joined from a terminal atrium does not hold is ended and
// resumed under atrium. The one place atrium ends a process it did not start: only on this click, only the card's pid.
func (d *Daemon) handleMoveIn(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t, err := d.st.Get(id)
	if err != nil {
		small.WriteJSONErr(w, http.StatusNotFound, err)
		return
	}
	m := d.moveInSeams()
	pid, started, err := d.moveInRunner(t, m)
	if err != nil {
		small.WriteJSONErr(w, http.StatusConflict, err)
		return
	}
	held, err := d.st.PendingForTask(id)
	if err != nil {
		small.WriteJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	for _, p := range held {
		if _, err := d.decide(p.ID, "block", MoveInReason, ""); err != nil {
			log.Printf("[atrium] move in %s: request %s was not answered: %v", t.DisplayTitle(), p.ID, err)
		}
	}
	began := time.Now()
	if started != "" {
		// The same process the check saw, or a reused pid this must not touch.
		if now, err := m.startTime(pid); err == nil && now == started {
			log.Printf("[atrium] move in %s: ending pid %d, the session joined from outside atrium", t.DisplayTitle(), pid)
			if err := m.stop(pid); err != nil {
				small.WriteJSONErr(w, http.StatusConflict, fmt.Errorf("could not end process %d: %w", pid, err))
				return
			}
		}
	}
	ended := time.Since(began)
	if err := d.moveInQuiet(id, pid, started, m); err != nil {
		small.WriteJSONErr(w, http.StatusConflict, err)
		return
	}
	log.Printf("[atrium] move in %s: ended in %s, quiet after %s, resuming", t.DisplayTitle(),
		ended.Round(time.Millisecond), time.Since(began).Round(time.Millisecond))
	m.resume(w, r)
}

// moveInQuiet waits for the ended process to be gone and its hooks to stop reaching the room, so the resumed session
// is the only one left talking for the card.
func (d *Daemon) moveInQuiet(id string, pid int, started string, m moveIn) error {
	deadline := time.Now().Add(m.limit)
	for {
		now, err := m.startTime(pid)
		gone := errors.Is(err, cardproc.ErrGone) || (err == nil && now != started)
		if gone && d.act.quietFor(id) >= m.quiet {
			return nil
		}
		if time.Now().After(deadline) {
			if !gone {
				return fmt.Errorf("process %d is still running after %s, so the card was not resumed", pid, m.limit)
			}
			return fmt.Errorf("the session's hooks still reach atrium after %s, so the card was not resumed", m.limit)
		}
		time.Sleep(m.poll)
	}
}
