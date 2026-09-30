package daemon

import (
	"fmt"
	"log"
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
// here and dies with the process; the next tick reads it again. What IS stored
// is the notice claim, so a restart does not tell a launcher twice.

// NoticeContext is the notice a launcher gets when its worker first passes the
// context threshold.
const NoticeContext = "context-size"

// ContextSize is a card's context as the board reads it.
type ContextSize struct {
	Tokens int64 `json:"tokens"`
	// Warn is whether it is at or past the threshold, worked out here so the
	// board and the notice cannot disagree about the line.
	Warn       bool `json:"warn"`
	ThresholdK int  `json:"threshold_k"`
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
}

func newContextSizes() *contextSizes {
	return &contextSizes{m: map[string]contextSeen{}, judged: map[string]int64{}, session: map[string]string{},
		transcript: api.TranscriptPath}
}

// started records the session a card's runner says it has just started.
//
// A `/clear` starts a new session on the same card, with a new transcript, and
// the resume id does not follow it until the new one has something written:
// the session hook keeps the old id, rightly, and only the next Stop moves it.
// Read by the resume id, the card sat on its old transcript's last size for
// that whole first turn, and the notice, claimed for the old id, did not come
// again for the new one (item 62, sa58: 151k, cleared, 336k with no word).
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
	limit := api.ContextThreshold(d.st)
	return &ContextSize{Tokens: s.tokens, Warn: s.tokens >= limit, ThresholdK: int(limit / 1000)}
}

// read is a card's context now, and whether it changed since the last read. A
// card with no transcript reads as zero.
func (c *contextSizes) read(t *store.Task) (int64, bool) {
	path := c.transcript(t.Worktree, c.sessionOf(t))
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

// watchContext reads every live Claude card's context size and tells an
// agent-launched card's launcher once when it passes the threshold.
//
// ONE NOTICE PER CROSSING. The claim is stored, so a restart that finds the
// card still past the line sends nothing. A card seen back under the line has
// its claim dropped, so a card that compacts and grows past it again is a new
// crossing. The claim is keyed on the session, so a card cleared onto a new
// one is a new crossing too. A human-started card is never noticed, only
// marked.
func (d *Daemon) watchContext() error {
	tasks, err := d.st.List(store.StatusRunning, store.StatusNeedsInput, store.StatusNeedsPermission)
	if err != nil {
		return err
	}
	limit := api.ContextThreshold(d.st)
	live, open := map[string]bool{}, map[string]bool{}
	for _, t := range tasks {
		open[t.ID] = true
		session := d.ctx.sessionOf(t)
		if session == "" || strings.TrimSpace(t.Worktree) == "" {
			continue
		}
		if h, err := d.st.Harness(t.Runner); err != nil || !isClaude(h) {
			continue
		}
		tokens, changed := d.ctx.read(t)
		if tokens == 0 {
			continue
		}
		live[t.ID] = true
		moved := d.ctx.judge(t.ID, limit)
		if !changed && !moved {
			continue
		}
		d.publishTask(t.ID)
		if !d.reportsToLauncher(t) {
			continue
		}
		if tokens < limit {
			if err := d.st.ForgetNotices(t.ID, NoticeContext); err != nil {
				log.Printf("[atrium] could not re-arm the context notice for %s: %v", t.DisplayTitle(), err)
			}
			continue
		}
		d.notifyLauncher(t, NoticeContext, session, fmt.Sprintf(
			"%s is at %dk context. Tell it to report what it has and stop, or hand off.",
			t.WireName, tokens/1000))
	}
	for _, id := range d.ctx.forgetExcept(live) {
		d.publishTask(id)
	}
	d.ctx.forgetSessions(open)
	return nil
}
