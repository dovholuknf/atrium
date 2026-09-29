package daemon

import (
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
// Claude Code subagents (the Task tool) are read at the same Stop, and what
// they spent since the last read is a row of its own, cause `subagent`. Newer
// Claude Code writes each to <session>/subagents/agent-<id>.jsonl, older wrote
// them into the main transcript marked `isSidechain`. Both are read, each file
// with its own cursor, one per message id, and no reply counted in the card's
// row is counted again. An atrium worker is a card of its own, with its own
// rows, and is never counted here.
//
// BEST EFFORT, like every hook path. A read or a write that fails is logged,
// and the turn goes on.

// usageSettle is how long after a Stop the transcript is read, so the turn's
// last lines are on disk. Only replies stamped before the Stop are counted, so
// a turn a blocked Stop continued is not folded into the one before it.
const usageSettle = 1500 * time.Millisecond

// usagePricesVersion names the price table a row's cost was worked out on.
const usagePricesVersion = "usageprices-2026-09-28b"

// usageOnlyPrices are the models a usage row prices that keep-alive must not
// refresh, because keepalivePrices is also the list of models keep-alive may
// refresh. Per million tokens, from
// https://platform.claude.com/docs/en/about-claude/pricing, fetched 2026-09-28.
// A 5m write is 1.25 times input, a read 0.1 times, on both.
var usageOnlyPrices = map[string]keepalivePrice{
	"claude-haiku-4-5": {In: 1, Cw1h: 2, Cr: 0.10, Out: 5},
	"claude-sonnet-5":  {In: 2, Cw1h: 4, Cr: 0.20, Out: 10},
	// Sonnet 5.5 costs what Sonnet 5 does, and is its own key because the
	// matcher below does not take one model's price for the other's.
	"claude-sonnet-5-5": {In: 2, Cw1h: 4, Cr: 0.20, Out: 10},
}

// usagePriceFor prices a reply for a usage row: keep-alive's models, then the
// others. An id matches its name alone or with a date stamp, so Sonnet 5's
// price is not taken for a Sonnet 5.5.
func usagePriceFor(model string) (keepalivePrice, bool) {
	if p, ok := keepalivePriceFor(model); ok {
		return p, true
	}
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.IndexByte(m, '['); i >= 0 {
		m = m[:i]
	}
	for k, p := range usageOnlyPrices {
		if m == k {
			return p, true
		}
		if date, ok := strings.CutPrefix(m, k+"-"); ok && len(date) == 8 && strings.Trim(date, "0123456789") == "" {
			return p, true
		}
	}
	return keepalivePrice{}, false
}

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
	subs   map[string]*subagentCursors

	// broadcast tells the board a row was written. Nil in tests and until the
	// daemon wires it. See emitRow.
	broadcast func(kind string, v any)
}

// usageEvent is one written row as the `usage` event carries it: the figures
// and the card id, and no message text. The hub adds the source room.
func usageEvent(row *store.SessionUsage) map[string]any {
	return map[string]any{
		"task_id": row.TaskID, "ended_at": row.Ended, "cause": row.Cause,
		"input": row.Input, "output": row.Output, "cache_write_5m": row.CacheWrite5m,
		"cache_write_1h": row.CacheWrite1h, "cache_read": row.CacheRead,
	}
}

// emitRow pushes a row that was just written, best effort like everything else
// on this path: the row is on record whether or not anybody hears of it.
func (u *usageTracker) emitRow(row *store.SessionUsage) {
	if u != nil && u.broadcast != nil && row != nil {
		u.broadcast("usage", usageEvent(row))
	}
}

// deptTagPrefix is the tag that files a card under a department.
const deptTagPrefix = "dept:"

// stamp fills a row's department and director from its card as it is now, so
// the row keeps them after the card is culled. Best effort: a card that cannot
// be read files under "".
func (u *usageTracker) stamp(row *store.SessionUsage) {
	if u == nil || row == nil {
		return
	}
	t, err := u.st.Get(row.TaskID)
	if err != nil || t == nil {
		return
	}
	row.Dept, row.Launcher, row.LauncherID = usageGroups(u.st, t)
}

// usageGroups is where a card's spend files. The department is the value of its
// `dept:<x>` tag, "" when none. The director is the card itself when tagged
// atrium:director, else the card that launched it, "" when the operator did.
// A director is named by its alias, else its handle.
func usageGroups(st *store.Store, t *store.Task) (dept, launcher, launcherID string) {
	for _, tag := range t.Tags {
		tag = strings.TrimSpace(tag)
		if len(tag) > len(deptTagPrefix) && strings.EqualFold(tag[:len(deptTagPrefix)], deptTagPrefix) {
			dept = strings.TrimSpace(tag[len(deptTagPrefix):])
			break
		}
	}
	name := func(c *store.Task) string {
		if c.Alias != "" {
			return c.Alias
		}
		return c.WireName
	}
	if hasTag(t.Tags, DirectorTag) {
		return dept, name(t), t.ID
	}
	if !t.Launched() {
		return dept, "", ""
	}
	if t.SpawnedByID != "" {
		if p, err := st.Get(t.SpawnedByID); err == nil && p != nil {
			return dept, name(p), p.ID
		}
	}
	return dept, t.SpawnedBy, t.SpawnedByID
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
		subs:    map[string]*subagentCursors{},
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
// one row for the replies stamped before the Stop, and one `subagent` row for
// what its subagents spent in the same time. It returns the main row, or nil
// when the turn made no request of its own.
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
	subs, err := u.subagentsOf(t, path)
	if err != nil {
		return nil, err
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
		main   = newReplySet()
		side   = newReplySet()
		beyond bool
	)
	read, err := scanReplies(f, func(r *mainReply) {
		if r.At.After(seg.stop) {
			beyond = true
			return
		}
		if r.Sidechain {
			// A subagent written inline, as older Claude Code did. It has its
			// own cursor, since its replies are not the card's.
			side.take(r, subs.inline)
			return
		}
		main.take(r, cur)
	})
	if err != nil {
		return nil, err
	}
	// Past a reply of the next turn, the next read starts here again. Its times
	// keep what this one counted from being counted twice.
	if !beyond {
		cur.offset += read
	}
	if err := readSubagentFiles(subs, seg.stop, side); err != nil {
		log.Printf("[atrium] could not read the subagents of %s: %v", t.ID, err)
	}
	var row *store.SessionUsage
	if len(main.order) == 0 {
		u.unspent(t.ID, seg)
	} else {
		row = main.row(t)
		row.Cause, row.AfterResume = seg.cause, seg.afterResume
		u.stamp(row)
		if err := u.st.AddSessionUsage(row); err != nil {
			// The next read starts again from the last row on record.
			delete(u.cursor, t.ID)
			delete(u.subs, t.ID)
			return nil, err
		}
		main.advance()
		u.emitRow(row)
	}
	// A reply is the card's or a subagent's, never both.
	side.drop(main)
	if len(side.order) > 0 {
		sub := side.row(t)
		sub.Cause = store.UsageSubagent
		u.stamp(sub)
		if err := u.st.AddSessionUsage(sub); err != nil {
			delete(u.subs, t.ID)
			return row, err
		}
		side.advance()
		u.emitRow(sub)
	}
	return row, nil
}

// subagentCursors is how far a session's subagent transcripts have been read.
type subagentCursors struct {
	session string
	// from is where a file seen for the first time is read from: the last
	// subagent row, or the daemon's start.
	from   usageCursor
	inline *usageCursor
	files  map[string]*usageCursor
}

func (u *usageTracker) subagentsOf(t *store.Task, path string) (*subagentCursors, error) {
	if s := u.subs[t.ID]; s != nil && s.session == path {
		return s, nil
	}
	s := &subagentCursors{session: path, from: usageCursor{lastAt: u.started}, files: map[string]*usageCursor{}}
	last, err := u.st.LastSubagentUsage(t.ID, t.ResumeID)
	if err != nil {
		return nil, err
	}
	if last != nil {
		s.from.lastAt, s.from.lastMsg = last.Ended, last.LastMessage
	}
	inline := s.from
	s.inline = &inline
	u.subs[t.ID] = s
	return s, nil
}

// subagentDir is where Claude Code writes a session's subagent transcripts:
// <session>/subagents/agent-<id>.jsonl beside <session>.jsonl, and a workflow's
// agents a level down.
func subagentDir(session string) string {
	return filepath.Join(strings.TrimSuffix(session, ".jsonl"), "subagents")
}

// readSubagentFiles reads every subagent transcript of the session from where
// its last read stopped, into set. A subagent is one conversation per file, so
// a file's times say what of it was counted, as the main file's do.
func readSubagentFiles(subs *subagentCursors, stop time.Time, set *replySet) error {
	dir := subagentDir(subs.session)
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "agent-") || !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		cur := subs.files[p]
		if cur == nil {
			cur = &usageCursor{path: p, lastAt: subs.from.lastAt, lastMsg: subs.from.lastMsg}
			subs.files[p] = cur
			if !info.ModTime().After(cur.lastAt) {
				// Last written before what was counted, so nothing in it is
				// new.
				cur.offset = info.Size()
				return nil
			}
		}
		if info.Size() < cur.offset {
			cur.offset = 0
		}
		if info.Size() == cur.offset {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer f.Close()
		if _, err := f.Seek(cur.offset, io.SeekStart); err != nil {
			return nil
		}
		beyond := false
		read, err := scanReplies(f, func(r *mainReply) {
			if r.At.After(stop) {
				beyond = true
				return
			}
			set.take(r, cur)
		})
		if err != nil {
			return err
		}
		if !beyond {
			cur.offset += read
		}
		return nil
	})
}

// replySet is the replies one read counts, one per message id, and the last
// one taken past each cursor.
type replySet struct {
	order []string
	byID  map[string]*mainReply
	from  map[*usageCursor]*mainReply
}

func newReplySet() *replySet {
	return &replySet{byID: map[string]*mainReply{}, from: map[*usageCursor]*mainReply{}}
}

// take counts a reply read past cur. A reply counted by an earlier read is
// skipped, and so is every later line of it. Lines of one reply read here all
// count as that reply.
func (s *replySet) take(r *mainReply, cur *usageCursor) {
	if r.MessageID == "" {
		r.MessageID = r.At.Format(time.RFC3339Nano)
	}
	_, seen := s.byID[r.MessageID]
	if !seen && (r.MessageID == cur.lastMsg || !r.At.After(cur.lastAt)) {
		return
	}
	if !seen {
		s.order = append(s.order, r.MessageID)
	}
	s.byID[r.MessageID] = r
	if last := s.from[cur]; last == nil || !r.At.Before(last.At) {
		s.from[cur] = r
	}
}

// drop leaves out every reply the other set counts.
func (s *replySet) drop(other *replySet) {
	kept := s.order[:0]
	for _, id := range s.order {
		if _, dup := other.byID[id]; !dup {
			kept = append(kept, id)
		}
	}
	s.order = kept
}

// advance moves each cursor past the last reply taken through it.
func (s *replySet) advance() {
	for cur, r := range s.from {
		cur.lastAt, cur.lastMsg = r.At, r.MessageID
	}
}

// row sums the set in time order, each reply priced on its own model, since a
// subagent often runs on a cheaper one.
func (s *replySet) row(t *store.Task) *store.SessionUsage {
	sort.SliceStable(s.order, func(i, j int) bool { return s.byID[s.order[i]].At.Before(s.byID[s.order[j]].At) })
	first, last := s.byID[s.order[0]], s.byID[s.order[len(s.order)-1]]
	row := &store.SessionUsage{
		TaskID: t.ID, ResumeID: t.ResumeID, Started: first.At, Ended: last.At,
		Model: last.Model, Replies: len(s.order), Context: last.Context(), LastMessage: last.MessageID,
	}
	for _, id := range s.order {
		r := s.byID[id]
		w5, w1 := r.Write5m, r.Write1h
		if w5+w1 < r.CacheWrite {
			// A reply with no split says nothing about the TTL. Claude Code's
			// own default is five minutes.
			w5 += r.CacheWrite - w5 - w1
		}
		one := &store.SessionUsage{Input: r.Input, Output: r.Output, CacheRead: r.CacheRead,
			CacheWrite5m: w5, CacheWrite1h: w1}
		row.Input += one.Input
		row.Output += one.Output
		row.CacheRead += one.CacheRead
		row.CacheWrite5m += w5
		row.CacheWrite1h += w1
		// Money is not worked out any more (item 37b). To bring it back:
		//   if p, ok := usagePriceFor(r.Model); ok {
		//       row.Cost += usageCost(one, p); row.Prices = usagePricesVersion }
		// and drop the `json:"-"` on the Cost fields in the store.
		_ = one
	}
	return row
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
		CacheRead: r.CacheRead, Context: r.Context,
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
