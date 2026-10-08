package daemon

import (
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

// A card's context size, read from its transcript on the reaper's tick.
//
// Every turn re-reads the whole context, so a worker at 300k pays for 300k on
// every turn, and neither the model nor the board says so. The size is the
// keep-alive's own figure, the last main reply's input plus cache writes plus
// cache reads, read by the same `readLastReply`.
//
// NEVER STORED, like activity (docs/runtime/activity-design.md). It lives in a map
// here and dies with the process; the next tick reads it again.
//
// NOBODY IS TOLD. The size marks the card, and past its limit the context cycle
// starts (autocontext.go). There is no notice to a launcher and no line on tool calls.

// ContextSize is a card's context as the board reads it.
type ContextSize struct {
	Tokens int64 `json:"tokens"`
	// Warn is whether it is at or past the card's limit, worked out here so the
	// board and the trigger cannot disagree about the line.
	Warn bool `json:"warn"`
	// ThresholdK is the card's limit in thousands, 0 for none.
	ThresholdK int `json:"threshold_k"`
	// Source is the layer ThresholdK came from: "card", "hub" or "default".
	Source string `json:"source"`
	// Cycle is whether atrium cycles this card's context at the limit.
	Cycle bool `json:"cycle"`
	// OwnK is the card's own limit as stored, 0 when it has none, so the board can show and clear it.
	OwnK int `json:"own_k,omitempty"`
	// CeilingK is the most the card's runner leaves room for, 0 when atrium cannot tell.
	CeilingK int `json:"ceiling_k,omitempty"`
	// WantedK and WantedFrom are the limit the card would have had without the ceiling, set only when the
	// ceiling cut it (Source is then "runner").
	WantedK    int    `json:"wanted_k,omitempty"`
	WantedFrom string `json:"wanted_from,omitempty"`
}

// contextSeen is one card's last read, and what the file looked like then, so
// a transcript that has not changed is not read again.
type contextSeen struct {
	path   string
	mod    time.Time
	size   int64
	tokens int64
}

type contextSizes struct {
	mu sync.Mutex
	m  map[string]contextSeen
	// judged is the threshold each card was last held against. A changed
	// threshold is a new look at an unchanged size: a card now under a raised
	// line is re-armed before it grows past it.
	judged map[string]int64
	// session is the session each card's runner last said it started, which
	// is newer than its resume id after a `/clear`. See started.
	session map[string]string
	// transcript finds a card's transcript. api.TranscriptPath, swapped in tests.
	transcript func(cwd, sessionID string) string
	// latest finds the newest session in a directory. api.LatestSession, swapped in tests.
	latest func(cwd string) string
}

func newContextSizes() *contextSizes {
	return &contextSizes{m: map[string]contextSeen{}, judged: map[string]int64{}, session: map[string]string{},
		transcript: api.TranscriptPath, latest: api.LatestSession}
}

// started records the session a card's runner says it has just started.
//
// A `/clear` starts a new session on the same card, with a new transcript, and
// the resume id does not follow it until the new one has something written:
// the session hook keeps the old id, rightly, and only the next Stop moves it.
// Read by the resume id, the card sat on its old transcript's last size for
// that whole first turn (item 62, sa58: 151k, cleared, 336k with no word).
func (c *contextSizes) started(id, sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.session[id] = sessionID
}

// sessionOf is the session to read a card's context from: the one its runner
// last started, or its resume id when none has been heard since this process
// began. NOT STORED: a restart resumes the card by its resume id, and the
// session hook says the id again when it comes up.
func (c *contextSizes) sessionOf(t *store.Task) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s := c.session[t.ID]; s != "" {
		return s
	}
	return strings.TrimSpace(t.ResumeID)
}

// judge records the threshold a card is held against, and reports whether it
// differs from the last one.
func (c *contextSizes) judge(id string, limit int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	was, ok := c.judged[id]
	c.judged[id] = limit
	return !ok || was != limit
}

// contextSizeFor is the board's view of a card's context, or an untyped nil.
func (d *Daemon) contextSizeFor(taskID string) any {
	d.ctx.mu.Lock()
	s, ok := d.ctx.m[taskID]
	d.ctx.mu.Unlock()
	if !ok {
		return nil
	}
	t, terr := d.st.Get(taskID)
	if terr != nil {
		t = nil
	}
	l := api.ContextLimitOf(d.st, t)
	limit := int64(l.K) * 1000
	return &ContextSize{Tokens: s.tokens, Warn: limit > 0 && s.tokens >= limit, ThresholdK: l.K, Source: l.From,
		Cycle: limit > 0 && d.cycleSubject(t), OwnK: l.OwnK, CeilingK: l.CeilingK, WantedK: l.WantedK,
		WantedFrom: l.WantedFrom}
}

// transcriptOf finds the transcript to read a card's context from, however the card started.
//
// The session its runner last said it started comes first, then its resume id. A resumed card or a fixture
// can have neither on disk yet (the runner's hook has not said, or the id is the one the resume replaced),
// so the newest transcript in its directory is the last resort, which is the one the runner is writing.
func (c *contextSizes) transcriptOf(t *store.Task) string {
	cwd := t.Worktree
	if p := c.transcript(cwd, c.sessionOf(t)); p != "" {
		return p
	}
	if p := c.transcript(cwd, t.ResumeID); p != "" {
		return p
	}
	if strings.TrimSpace(cwd) == "" {
		return ""
	}
	return c.transcript(cwd, c.latest(cwd))
}

// read is a card's context now, and whether it changed since the last read. A
// card with no transcript reads as zero.
func (c *contextSizes) read(t *store.Task) (int64, bool) {
	path := c.transcriptOf(t)
	if path == "" {
		return 0, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	c.mu.Lock()
	last, ok := c.m[t.ID]
	c.mu.Unlock()
	if ok && last.path == path && last.mod.Equal(info.ModTime()) && last.size == info.Size() {
		return last.tokens, false
	}
	r, err := readLastReply(path)
	if err != nil {
		return 0, false
	}
	c.mu.Lock()
	c.m[t.ID] = contextSeen{path: path, mod: info.ModTime(), size: info.Size(), tokens: r.Context}
	c.mu.Unlock()
	return r.Context, !ok || last.tokens != r.Context
}

// forgetSessions drops the started session of every card not in open.
func (c *contextSizes) forgetSessions(open map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id := range c.session {
		if !open[id] {
			delete(c.session, id)
		}
	}
}

// forgetExcept drops every card not in live, and returns the ones it dropped.
func (c *contextSizes) forgetExcept(live map[string]bool) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var gone []string
	for id := range c.m {
		if !live[id] {
			delete(c.m, id)
			delete(c.judged, id)
			gone = append(gone, id)
		}
	}
	return gone
}

// watchContext reads every live Claude card's context size, marks the card, and
// holds it to its limit: past it, the context cycle starts, and a cycle already on
// the card is poked so a prompt that is due goes now. See autocontext.go.
func (d *Daemon) watchContext() error {
	tasks, err := d.st.List(store.StatusRunning, store.StatusNeedsInput, store.StatusNeedsPermission)
	if err != nil {
		return err
	}
	live, open := map[string]bool{}, map[string]bool{}
	for _, t := range tasks {
		open[t.ID] = true
		if strings.TrimSpace(t.Worktree) == "" {
			continue
		}
		if h, err := d.st.Harness(t.Runner); err != nil || !isClaude(h) {
			continue
		}
		// For a runner whose hooks are not wired. See outputat.go.
		if d.outputMoved(t) {
			d.publishTask(t.ID)
		}
		tokens, changed := d.ctx.read(t)
		if tokens == 0 {
			// A card just cleared reads as nothing until its new conversation has a reply.
			continue
		}
		live[t.ID] = true
		// Every tick and not only when the size changed: a cycle in flight waits on
		// turns, which move by themselves.
		d.cycleCheck(t, tokens)
		limitK, _ := api.ContextLimitFor(d.st, t)
		if moved := d.ctx.judge(t.ID, int64(limitK)*1000); changed || moved {
			d.publishTask(t.ID)
		}
	}
	for _, id := range d.ctx.forgetExcept(live) {
		d.publishTask(id)
	}
	d.ctx.forgetSessions(open)
	d.forgetOutput(open)
	return nil
}
