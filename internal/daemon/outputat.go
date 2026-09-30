package daemon

import (
	"bytes"
	"io"
	"os"
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
// read runs off it, a moment later so the line the runner is writing has landed.
// The reaper's tick checks too, for a runner whose hooks are not wired.
//
// READ FROM WHERE THE LAST READ STOPPED. Every tool result grows the transcript,
// so a cache on size and mtime would miss on nearly every check and re-read the
// whole tail each time. Only complete lines are taken, so a line half written is
// read whole on the next check. A new session, or a file that shrank, is read
// from its tail again.
//
// NEVER STORED, like activity. After a restart the first check finds the time
// again.

// outputCheckDelay is how long after a hook the transcript is read.
const outputCheckDelay = 400 * time.Millisecond

type outputTimes struct {
	mu      sync.Mutex
	seen    map[string]outputSeen
	pending map[string]*time.Timer
	closed  bool
}

// outputSeen is one card's last read: which transcript, how far into it, and the
// newest reply found so far.
type outputSeen struct {
	path   string
	offset int64
	at     time.Time
}

// outputSoon arms one check for a card, unless one is already armed.
func (d *Daemon) outputSoon(taskID string) {
	o := &d.output
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || o.pending[taskID] != nil {
		return
	}
	if o.pending == nil {
		o.pending = map[string]*time.Timer{}
	}
	o.pending[taskID] = time.AfterFunc(outputCheckDelay, func() {
		o.mu.Lock()
		delete(o.pending, taskID)
		closed := o.closed
		o.mu.Unlock()
		if closed {
			return
		}
		t, err := d.st.Get(taskID)
		if err != nil || t == nil {
			return
		}
		if d.outputMoved(t) {
			d.publishTask(taskID)
		}
	})
}

// stopOutput cancels every armed check, so none reads a transcript after the
// daemon is closed.
func (d *Daemon) stopOutput() {
	o := &d.output
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed = true
	for id, tm := range o.pending {
		tm.Stop()
		delete(o.pending, id)
	}
}

// outputMoved reads what a card's transcript gained since the last read and
// reports whether its newest reply moved.
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
	o := &d.output
	o.mu.Lock()
	prev, have := o.seen[t.ID]
	o.mu.Unlock()
	if have && prev.path != path {
		have = false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	start := int64(0)
	switch {
	case have && info.Size() == prev.offset:
		return false
	case have && info.Size() > prev.offset:
		start = prev.offset
	}
	// NEVER MORE THAN THE TAIL, a first read or not. Only the newest reply
	// matters, and a pasted image stored in one line can put megabytes between
	// two checks. A window that starts mid-line skips that line, which does not
	// parse.
	if info.Size()-start > transcriptTail {
		start = info.Size() - transcriptTail
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return false
	}
	chunk, err := io.ReadAll(io.LimitReader(f, info.Size()-start))
	if err != nil {
		return false
	}
	// Complete lines only. The rest is read again next time.
	chunk = chunk[:bytes.LastIndexByte(chunk, '\n')+1]
	// A scan that fails still moves the offset past what it was given, or the
	// same bytes would fail on every check and output_at would never move again.
	replies, _ := scanReplyText(bytes.NewReader(chunk))
	next := outputSeen{path: path, offset: start + int64(len(chunk))}
	if have {
		next.at = prev.at
	}
	moved := false
	for _, r := range replies {
		if !r.At.IsZero() && r.At.After(next.at) {
			next.at, moved = r.At, true
		}
	}
	o.mu.Lock()
	if o.seen == nil {
		o.seen = map[string]outputSeen{}
	}
	o.seen[t.ID] = next
	o.mu.Unlock()
	return moved
}

// outputAtFor is the board's view: RFC3339, or "" when nothing is known.
func (d *Daemon) outputAtFor(taskID string) string {
	o := &d.output
	o.mu.Lock()
	defer o.mu.Unlock()
	s, ok := o.seen[taskID]
	if !ok || s.at.IsZero() {
		return ""
	}
	return s.at.UTC().Format(time.RFC3339Nano)
}

// forgetOutput drops the cards not in open.
func (d *Daemon) forgetOutput(open map[string]bool) {
	o := &d.output
	o.mu.Lock()
	defer o.mu.Unlock()
	for id := range o.seen {
		if !open[id] {
			delete(o.seen, id)
		}
	}
}
