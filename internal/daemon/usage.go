package daemon

import (
	"errors"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

// TOKEN USE ON RECORD. Every Claude card's spend, a row per turn, from the
// runner's own transcript, with what started the turn where atrium can tell.
// Kept in session_usage, shown only in a card's details. See
// docs/backlog-2.md item 37.
//
// A turn is what lies between two Stop hooks. At each Stop the replies written
// since the last one are read with the keep-alive's scanner, one per message id,
// summed, and written as one row. The cause is decided before the turn starts,
// from what atrium typed or saw submitted:
//
//   - a prompt atrium submitted with a peer's banner is a `say`, and one with
//     the restart or exit wake's label is a `restart-wake`;
//   - any other prompt is the `operator`'s;
//   - a Stop the hook blocks to deliver messages starts a `say`, or an
//     `operator` turn when every message was the operator's;
//   - no prompt at all is `unknown`, a subagent's report waking the session;
//   - the first turn of a runner started on a resume is flagged, and called a
//     `resume` when nothing else claims it.
//
// Keep-alive refreshes are forks and never reach the card's transcript. Their
// rows come from the fork's receipt in keepalive.go.
//
// BEST EFFORT, like every hook path. A read or a write that fails is logged,
// and the turn goes on.

// usageSettle is how long after a Stop the transcript is read, so the turn's
// last lines are on disk. Only replies stamped before the Stop are counted, so
// a turn a blocked Stop continued is not folded into the one before it.
const usageSettle = 1500 * time.Millisecond

// usagePricesVersion names the price table a row's cost was worked out on.
const usagePricesVersion = keepalivePricesVersion

// usageSegment is what is known about a turn when its Stop arrives.
type usageSegment struct {
	cause       string
	afterResume bool
	stop        time.Time
}

// usageCursor is how far a card's transcript has been read.
type usageCursor struct {
	path    string
	offset  int64
	lastAt  time.Time
	lastMsg string
}

type usageTracker struct {
	st         *store.Store
	transcript func(cwd, sessionID string) string
	isClaude   func(runner string) bool
	// started is when this daemon started. A card with no row yet counts only
	// what it spent from here, rather than a month of transcript as one turn.
	started time.Time
	settle  time.Duration

	mu      sync.Mutex
	cause   map[string]string
	resumed map[string]bool

	// readMu serialises reads, so two Stops close together cannot count the
	// same replies twice.
	readMu sync.Mutex
	cursor map[string]*usageCursor
}

func newUsageTracker(st *store.Store) *usageTracker {
	return &usageTracker{
		st:         st,
		transcript: api.TranscriptPath,
		isClaude: func(runner string) bool {
			h, err := st.Harness(runner)
			return err == nil && isClaude(h)
		},
		started: time.Now().UTC(),
		settle:  usageSettle,
		cause:   map[string]string{},
		resumed: map[string]bool{},
		cursor:  map[string]*usageCursor{},
	}
}

// launched is a runner starting on a card. A resume flags the first turn.
func (u *usageTracker) launched(taskID string, resumed bool) {
	if u == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.cause, taskID)
	if resumed {
		u.resumed[taskID] = true
	} else {
		delete(u.resumed, taskID)
	}
}

// prompted is what started the turn that is starting.
func (u *usageTracker) prompted(taskID, cause string) {
	if u == nil {
		return
	}
	u.mu.Lock()
	u.cause[taskID] = cause
	u.mu.Unlock()
}

// endSegment takes the cause of the turn a Stop just ended, and clears it for
// the next one.
func (u *usageTracker) endSegment(taskID string, at time.Time) usageSegment {
	u.mu.Lock()
	defer u.mu.Unlock()
	seg := usageSegment{cause: u.cause[taskID], afterResume: u.resumed[taskID], stop: at}
	delete(u.cause, taskID)
	delete(u.resumed, taskID)
	if seg.cause == "" {
		seg.cause = store.UsageUnknown
	}
	if seg.afterResume && (seg.cause == store.UsageOperator || seg.cause == store.UsageUnknown) {
		seg.cause = store.UsageResume
	}
	return seg
}

// unspent puts a resume flag back when its Stop found nothing to record, so it
// lands on the turn that did spend.
func (u *usageTracker) unspent(taskID string, seg usageSegment) {
	if !seg.afterResume {
		return
	}
	u.mu.Lock()
	if _, launched := u.resumed[taskID]; !launched {
		u.resumed[taskID] = true
	}
	u.mu.Unlock()
}

// stopped is a Stop hook: the turn is read and recorded once it has settled.
func (u *usageTracker) stopped(t *store.Task) {
	if u == nil || t == nil || t.ResumeID == "" || t.Worktree == "" || !u.isClaude(t.Runner) {
		return
	}
	seg := u.endSegment(t.ID, time.Now().UTC())
	task := *t
	go func() {
		time.Sleep(u.settle)
		if _, err := u.record(&task, seg); err != nil {
			log.Printf("[atrium] could not record token use on %s: %v", task.ID, err)
		}
	}()
}

// record reads a card's transcript from where the last read stopped, and writes
// one row for the replies stamped before the Stop. It returns the row, or nil
// when the turn made no request.
func (u *usageTracker) record(t *store.Task, seg usageSegment) (*store.SessionUsage, error) {
	u.readMu.Lock()
	defer u.readMu.Unlock()
	path := u.transcript(t.Worktree, t.ResumeID)
	if path == "" {
		u.unspent(t.ID, seg)
		return nil, nil
	}
	cur := u.cursor[t.ID]
	if cur == nil || cur.path != path {
		// A new card, a new session, or a daemon that just started: from the
		// last row this session wrote, or from now.
		cur = &usageCursor{path: path, lastAt: u.started}
		last, err := u.st.LastTranscriptUsage(t.ID, t.ResumeID)
		if err != nil {
			return nil, err
		}
		if last != nil {
			cur.lastAt, cur.lastMsg = last.Ended, last.LastMessage
		}
		u.cursor[t.ID] = cur
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() < cur.offset {
		// Rewritten under us. The times still say what was counted.
		cur.offset = 0
	}
	if _, err := f.Seek(cur.offset, io.SeekStart); err != nil {
		return nil, err
	}
	var (
		order  []string
		byID   = map[string]*mainReply{}
		beyond bool
	)
	read, err := scanMainReplies(f, func(r *mainReply) {
		if r.At.After(seg.stop) {
			beyond = true
			return
		}
		if r.MessageID == "" {
			r.MessageID = r.At.Format(time.RFC3339Nano)
		}
		// A reply counted by an earlier read is skipped, and so is every later
		// line of it. Lines of one reply read here all count as that reply.
		_, seen := byID[r.MessageID]
		if !seen && (r.MessageID == cur.lastMsg || !r.At.After(cur.lastAt)) {
			return
		}
		if !seen {
			order = append(order, r.MessageID)
		}
		byID[r.MessageID] = r
	})
	if err != nil {
		return nil, err
	}
	// Past a reply of the next turn, the next read starts here again. Its times
	// keep what this one counted from being counted twice.
	if !beyond {
		cur.offset += read
	}
	if len(order) == 0 {
		u.unspent(t.ID, seg)
		return nil, nil
	}
	first, last := byID[order[0]], byID[order[len(order)-1]]
	row := &store.SessionUsage{
		TaskID: t.ID, ResumeID: t.ResumeID, Started: first.At, Ended: last.At,
		Cause: seg.cause, AfterResume: seg.afterResume, Model: last.Model, Replies: len(order),
		Context: last.Context(), LastMessage: last.MessageID,
	}
	for _, id := range order {
		r := byID[id]
		row.Input += r.Input
		row.Output += r.Output
		row.CacheRead += r.CacheRead
		w5, w1 := r.Write5m, r.Write1h
		if w5+w1 < r.CacheWrite {
			// A reply with no split says nothing about the TTL. Claude Code's
			// own default is five minutes.
			w5 += r.CacheWrite - w5 - w1
		}
		row.CacheWrite5m += w5
		row.CacheWrite1h += w1
	}
	if p, ok := keepalivePriceFor(last.Model); ok {
		row.Cost = usageCost(row, p)
		row.Prices = usagePricesVersion
	}
	cur.lastAt, cur.lastMsg = last.At, last.MessageID
	if err := u.st.AddSessionUsage(row); err != nil {
		return nil, err
	}
	return row, nil
}

// usageCost prices a row. A 5m write is 1.25 times input, the 1h rate is in
// the table.
func usageCost(u *store.SessionUsage, p keepalivePrice) float64 {
	return (float64(u.Input)*p.In + float64(u.CacheWrite5m)*p.In*1.25 + float64(u.CacheWrite1h)*p.Cw1h +
		float64(u.CacheRead)*p.Cr + float64(u.Output)*p.Out) / 1e6
}

// usageOfRefresh is a keep-alive refresh as a usage row. The fork writes at
// the 1h rate, because only cards on the 1h cache are refreshed.
func usageOfRefresh(r *store.KeepaliveRefresh) *store.SessionUsage {
	return &store.SessionUsage{
		TaskID: r.TaskID, ResumeID: r.ResumeID, Started: r.At, Ended: r.At, Cause: store.UsageKeepalive,
		Model: r.Model, Replies: 1, Input: r.Input, Output: r.Output, CacheWrite1h: r.CacheWrite,
		CacheRead: r.CacheRead, Context: r.Context, Cost: r.Cost, Prices: r.Prices,
	}
}

// promptCause is what a prompt submitted now was: the peer message or wake
// atrium just typed, or the operator.
func (d *Daemon) promptCause(taskID string) string {
	if run := d.sup.get(taskID); run != nil {
		if c := run.peerPromptCause(time.Now()); c != "" {
			return c
		}
	}
	return store.UsageOperator
}

// causeOfBanner is what a turn atrium typed in with this label is.
func causeOfBanner(banner string) string {
	if banner == wakeLabel || banner == exitLabel {
		return store.UsageRestartWake
	}
	return store.UsageSay
}

// messagesCause is a turn a blocked Stop starts with these messages.
func messagesCause(msgs []*store.Message) string {
	for _, m := range msgs {
		if !m.FromHuman() {
			return store.UsageSay
		}
	}
	return store.UsageOperator
}

// UsageView is what a card's details show.
type UsageView struct {
	Totals  *store.UsageTotals            `json:"totals"`
	ByCause map[string]*store.UsageTotals `json:"by_cause"`
	// ContextNow is the context of the last main reply in the transcript,
	// read now. Zero when there is none.
	ContextNow int64                 `json:"context_now"`
	Model      string                `json:"model,omitempty"`
	Rows       []*store.SessionUsage `json:"rows"`
}

// usageFor is a card's usage view.
func (d *Daemon) usageFor(taskID string, limit int) (any, error) {
	if d.usage == nil {
		return nil, errors.New("token use is not recorded here")
	}
	all, byCause, err := d.st.SessionUsageTotals(taskID)
	if err != nil {
		return nil, err
	}
	rows, err := d.st.SessionUsageOf(taskID, limit)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []*store.SessionUsage{}
	}
	v := &UsageView{Totals: all, ByCause: byCause, Rows: rows}
	if t, err := d.st.Get(taskID); err == nil && t != nil && t.ResumeID != "" {
		if path := d.usage.transcript(t.Worktree, t.ResumeID); path != "" {
			if r, err := readLastReply(path); err == nil {
				v.ContextNow, v.Model = r.Context, r.Model
			}
		}
	}
	if v.ContextNow == 0 && len(rows) > 0 {
		v.ContextNow, v.Model = rows[0].Context, rows[0].Model
	}
	return v, nil
}
