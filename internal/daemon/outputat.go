package daemon

import (
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// output_at: when a card's transcript last gained an assistant reply with text,
// mid-turn too. The phone's card view (/m) reads a card's replies on open and at
// turn end, so a long turn looked frozen. With this on the task event it re-reads
// `/replies` when the time moves.
//
// CHECKED ON EVERY ACTIVITY HOOK, coalesced. A reply is followed by a tool call or
// by the turn's end, and both fire a hook. The hook path only arms a timer. The
// read runs off it, a moment later so the line the runner is writing has landed,
// and it is `readReplies`, cached on the transcript's size and mtime. The reaper's
// tick checks too, for a runner whose hooks are not wired.
//
// NEVER STORED, like activity. After a restart the first check finds the time
// again.

// outputCheckDelay is how long after a hook the transcript is read.
const outputCheckDelay = 400 * time.Millisecond

type outputTimes struct {
	mu      sync.Mutex
	at      map[string]time.Time
	pending map[string]bool
}

// soon arms one check for a card, unless one is already armed.
func (d *Daemon) outputSoon(taskID string) {
	o := &d.output
	o.mu.Lock()
	if o.pending == nil {
		o.pending = map[string]bool{}
	}
	if o.pending[taskID] {
		o.mu.Unlock()
		return
	}
	o.pending[taskID] = true
	o.mu.Unlock()
	time.AfterFunc(outputCheckDelay, func() {
		o.mu.Lock()
		delete(o.pending, taskID)
		o.mu.Unlock()
		t, err := d.st.Get(taskID)
		if err != nil || t == nil {
			return
		}
		if d.outputMoved(t) {
			d.publishTask(taskID)
		}
	})
}

// outputMoved reads a card's last reply and reports whether its time moved.
func (d *Daemon) outputMoved(t *store.Task) bool {
	if d.usage == nil || !d.usage.isClaude(t.Runner) {
		return false
	}
	session := strings.TrimSpace(t.ResumeID)
	if d.ctx != nil {
		session = d.ctx.sessionOf(t)
	}
	if session == "" {
		return false
	}
	path := d.usage.transcript(t.Worktree, session)
	if path == "" {
		return false
	}
	replies, err := readReplies(path, 1)
	if err != nil || len(replies) == 0 || replies[0].At.IsZero() {
		return false
	}
	at := replies[0].At
	o := &d.output
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.at == nil {
		o.at = map[string]time.Time{}
	}
	if was, ok := o.at[t.ID]; ok && !at.After(was) {
		return false
	}
	o.at[t.ID] = at
	return true
}

// outputAtFor is the board's view: RFC3339, or "" when nothing is known.
func (d *Daemon) outputAtFor(taskID string) string {
	o := &d.output
	o.mu.Lock()
	defer o.mu.Unlock()
	at, ok := o.at[taskID]
	if !ok {
		return ""
	}
	return at.UTC().Format(time.RFC3339Nano)
}

// forgetOutput drops the cards not in open.
func (d *Daemon) forgetOutput(open map[string]bool) {
	o := &d.output
	o.mu.Lock()
	defer o.mu.Unlock()
	for id := range o.at {
		if !open[id] {
			delete(o.at, id)
		}
	}
}
