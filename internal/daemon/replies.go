package daemon

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// GET /v1/tasks/{id}/replies: a card's last few replies as text, for the phone's
// conversation page (/m, M2 in docs/backlog/ui/mobile-design.md, r-024).
//
// READ FROM THE CONVERSATION THE RUNNER LAST STARTED (`d.ctx.sessionOf`), not the
// stored resume id, which after a /clear still names the old one until the new
// session's first Stop (r-021 part 1's bug).
//
// MAIN-CHAIN ASSISTANT TEXT ONLY. No tool calls, no thinking, no subagent
// (`isSidechain`) replies. A reply written in several content blocks shares one
// message id, and its text blocks are joined.
//
// BOUNDED TWICE. The transcript is read from its last `transcriptTail` bytes,
// as the keep-alive's read is, and each reply is cut at `replyTextMax` with
// `truncated` set. A card atrium cannot read a transcript for (codex, or no file)
// answers with its screen's rows as one reply, `source: "screen"`.
//
// PAGING (r-replies-paging). `?before=<at>` asks for the n replies and n prompts
// strictly older than `at`, read backwards from the end in `repliesWindow` steps
// and never past `repliesReach` bytes. `more` says whether anything older is left.

const (
	repliesDefault = 3
	repliesMax     = 50
	// replyTextMax cuts one reply, so a long answer cannot make the page heavy.
	replyTextMax = 16 << 10
)

// Variables so a test can use small windows, as autoTiming does.
var (
	// repliesWindow is the first page's read, from the end, and the step of a
	// `before` page's backward read.
	repliesWindow int64 = transcriptTail
	// repliesReach is the most a `before` page reads.
	repliesReach int64 = 16 << 20
)

// Reply is one reply as the page draws it.
type Reply struct {
	At        time.Time `json:"at"`
	Text      string    `json:"text"`
	Truncated bool      `json:"truncated"`
}

// RepliesView is the answer: where the text came from, and the replies oldest first.
type RepliesView struct {
	Source  string  `json:"source"`
	Replies []Reply `json:"replies"`
	// Prompts is what was said TO the card, from every source: typed at its
	// terminal, sent from the desktop board or the phone, or typed by atrium for
	// a peer. Oldest first, bounded like replies. Absent when atrium reads the
	// screen, which cannot tell a prompt from output.
	Prompts []Prompt `json:"prompts,omitempty"`
	// More is true when replies or prompts older than the oldest in this page
	// may exist. Never set for the screen.
	More bool `json:"more"`
	// NextBefore is the `before` of the next page, RFC3339Nano, set when More is
	// true. Both lists are complete down to it, so a client passes it back as it
	// is and never works out a cursor from the entries.
	NextBefore string `json:"next_before,omitempty"`
}

// Prompt kinds. See promptOf.
const (
	PromptOperator = "operator"
	PromptPeer     = "peer"
	PromptCommand  = "command"
)

// Prompt is one user turn as the page draws it.
type Prompt struct {
	At        time.Time `json:"at"`
	Text      string    `json:"text"`
	Truncated bool      `json:"truncated"`
	Kind      string    `json:"kind"`
}

// errNoSuchCard is a card this room does not hold, which the API answers 404.
var errNoSuchCard = sql.ErrNoRows

// repliesFor answers the endpoint for one card, first page.
func (d *Daemon) repliesFor(taskID string, n int) (*RepliesView, error) {
	return d.repliesPage(taskID, n, time.Time{})
}

// repliesPage answers the endpoint for one card. A zero before is the first page.
func (d *Daemon) repliesPage(taskID string, n int, before time.Time) (*RepliesView, error) {
	t, err := d.st.Get(taskID)
	if err != nil {
		// Only a missing row is "no such card". Any other store error is the
		// store's, and the route answers it as one.
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNoSuchCard
		}
		return nil, err
	}
	if t == nil {
		return nil, errNoSuchCard
	}
	switch {
	case n <= 0:
		n = repliesDefault
	case n > repliesMax:
		n = repliesMax
	}
	if d.usage != nil && d.usage.isClaude(t.Runner) {
		session := strings.TrimSpace(t.ResumeID)
		if d.ctx != nil {
			session = d.ctx.sessionOf(t)
		}
		if session != "" {
			if path := d.usage.transcript(t.Worktree, session); path != "" {
				var (
					pg  replyPage
					err error
				)
				if before.IsZero() {
					pg, err = readTranscriptPage(path, n)
				} else {
					pg, err = readTranscriptBefore(path, n, before)
				}
				if err == nil {
					v := &RepliesView{Source: "transcript", Replies: pg.replies, Prompts: pg.prompts, More: pg.more}
					if pg.more {
						v.NextBefore = pg.next.UTC().Format(time.RFC3339Nano)
					}
					return v, nil
				}
				logRepliesFallback(path, err)
			}
		}
	}
	return &RepliesView{Source: "screen", Replies: d.screenReply(taskID)}, nil
}

// repliesFellBack holds the transcripts already logged as unreadable, so a page
// that reads on every turn end says why once and not every time.
var repliesFellBack sync.Map

func logRepliesFallback(path string, err error) {
	if _, seen := repliesFellBack.LoadOrStore(path, true); !seen {
		log.Printf("[atrium] replies: cannot read transcript %s, answering from the screen: %v", path, err)
	}
}

// screenReply is the card's screen as text, as one reply: the codex card and the
// card with no readable transcript. Empty when atrium holds no terminal for it.
func (d *Daemon) screenReply(taskID string) []Reply {
	run := d.sup.get(taskID)
	if run == nil {
		return []Reply{}
	}
	backlog, cuts, rows, _ := run.buf.ReplayCuts()
	text := strings.TrimSpace(string(stripSGR(replayCut(backlog, "screen", cuts, rows))))
	if text == "" {
		return []Reply{}
	}
	// The END of the screen is the latest, so a cut keeps the tail.
	cut, truncated := keepTail(text, replyTextMax)
	return []Reply{{At: time.Now().UTC(), Text: cut, Truncated: truncated}}
}

// repliesCache keeps the last read per transcript on its size and modification
// time, since the page reads on load and at every turn end and a transcript only
// grows. Its own lock.
var repliesCache = struct {
	mu sync.Mutex
	m  map[string]repliesCached
}{m: map[string]repliesCached{}}

type repliesCached struct {
	size    int64
	mtime   time.Time
	replies []Reply  // the last repliesMax, oldest first
	prompts []Prompt // the same, for what was said to the card
	floor   time.Time // zero when everything older is held, else complete down to here. See finishPage
}

// replyPage is one answer: the replies and prompts, and where the next page starts.
type replyPage struct {
	replies []Reply
	prompts []Prompt
	more    bool
	next    time.Time // the `before` of the next page, set when more is true
}

// finishPage cuts a page from rs and ps, every entry older than the request's
// before, oldest first. floor is the oldest time the read is complete down to
// (zero when it reached the start of the file with nothing cut).
//
// Each list keeps its n newest. A list that had more than n puts its oldest kept
// `at` in as a floor too, and the page is cut at the NEWEST floor, in both lists:
// the page is complete down to there, so the next page asks strictly older than
// it and nothing is lost or shown twice. Entries sharing the cut's time stay
// together on this page.
func finishPage(rs []Reply, ps []Prompt, n int, floor time.Time) replyPage {
	cut := floor
	rs, rcut := trimReplies(rs, n)
	ps, pcut := trimPrompts(ps, n)
	for _, c := range []time.Time{rcut, pcut} {
		if c.After(cut) {
			cut = c
		}
	}
	if cut.IsZero() {
		return replyPage{replies: rs, prompts: ps}
	}
	for len(rs) > 0 && rs[0].At.Before(cut) {
		rs = rs[1:]
	}
	for len(ps) > 0 && ps[0].At.Before(cut) {
		ps = ps[1:]
	}
	return replyPage{replies: rs, prompts: ps, more: true, next: cut}
}

// trimReplies keeps the n newest, plus any older ones at the same time as the
// oldest kept. The second result is that oldest time when something was cut.
func trimReplies(x []Reply, n int) ([]Reply, time.Time) {
	i := len(x) - n
	if i <= 0 {
		return x, time.Time{}
	}
	for i > 0 && x[i-1].At.Equal(x[i].At) {
		i--
	}
	if i == 0 {
		return x, time.Time{}
	}
	return x[i:], x[i].At
}

func trimPrompts(x []Prompt, n int) ([]Prompt, time.Time) {
	i := len(x) - n
	if i <= 0 {
		return x, time.Time{}
	}
	for i > 0 && x[i-1].At.Equal(x[i].At) {
		i--
	}
	if i == 0 {
		return x, time.Time{}
	}
	return x[i:], x[i].At
}

func laterOf(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// readReplies is the last n main-chain assistant replies with text in a
// transcript, oldest first.
func readReplies(path string, n int) ([]Reply, error) {
	replies, _, err := readTranscriptText(path, n)
	return replies, err
}

// readTranscriptText is the last n replies and the last n prompts in a
// transcript, each oldest first.
func readTranscriptText(path string, n int) ([]Reply, []Prompt, error) {
	pg, err := readTranscriptPage(path, n)
	return pg.replies, pg.prompts, err
}

// readTranscriptPage is the first page: the newest n of each, from the cached
// read of the file's last repliesWindow bytes.
func readTranscriptPage(path string, n int) (replyPage, error) {
	info, err := os.Stat(path)
	if err != nil {
		return replyPage{}, err
	}
	repliesCache.mu.Lock()
	c, ok := repliesCache.m[path]
	repliesCache.mu.Unlock()
	if !ok || c.size != info.Size() || !c.mtime.Equal(info.ModTime()) {
		f, err := os.Open(path)
		if err != nil {
			return replyPage{}, err
		}
		defer f.Close()
		mid := info.Size() > repliesWindow
		if mid {
			if _, err := f.Seek(info.Size()-repliesWindow, io.SeekStart); err != nil {
				return replyPage{}, err
			}
		}
		replies, prompts, firstAt, err := scanTranscriptWindow(f)
		if err != nil {
			return replyPage{}, err
		}
		var floor time.Time
		if mid {
			floor = boundaryAt(f, info.Size()-repliesWindow, firstAt)
		}
		if len(replies) > repliesMax {
			replies = replies[len(replies)-repliesMax:]
			floor = laterOf(floor, replies[0].At)
		} else if mid && len(replies) > 1 && !replies[0].At.Equal(replies[1].At) {
			// The oldest may have lost its earlier blocks to the cut, and a `before`
			// page would then return it again whole. It is the next page's first.
			replies = replies[1:]
			floor = laterOf(floor, replies[0].At)
		}
		if len(prompts) > repliesMax {
			prompts = prompts[len(prompts)-repliesMax:]
			floor = laterOf(floor, prompts[0].At)
		}
		c = repliesCached{size: info.Size(), mtime: info.ModTime(), replies: replies, prompts: prompts, floor: floor}
		repliesCache.mu.Lock()
		if len(repliesCache.m) > 400 {
			repliesCache.m = map[string]repliesCached{}
		}
		repliesCache.m[path] = c
		repliesCache.mu.Unlock()
	}
	pg := finishPage(c.replies, c.prompts, n, c.floor)
	pg.replies, pg.prompts = append([]Reply{}, pg.replies...), append([]Prompt{}, pg.prompts...)
	return pg, nil
}

// boundaryAt is the time a read starting at offset start is complete down to:
// the first record in it, else the first timestamped line, else now, which makes
// the next page ask for everything and read back from the end.
func boundaryAt(f *os.File, start int64, firstAt time.Time) time.Time {
	if !firstAt.IsZero() {
		return firstAt
	}
	if _, at, ok := probeLine(f, start-1); ok {
		return at
	}
	return time.Now().UTC()
}

// readTranscriptBefore is the n replies and n prompts strictly older than
// before, each oldest first, read backwards from the end of the file in
// repliesWindow steps until both lists are full, repliesReach bytes are read, or
// the start is reached. more is true unless the start was reached with nothing
// older left.
//
// A step starts mid-line, so its first partial line is carried and put back on
// the end of the next step, which makes a line read whole once. A reply whose
// message id spans steps is joined as the forward scan joins it.
func readTranscriptBefore(path string, n int, before time.Time) (replyPage, error) {
	f, err := os.Open(path)
	if err != nil {
		return replyPage{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return replyPage{}, err
	}
	var (
		firstAt time.Time // the oldest record read
		replies []Reply
		ids     []string
		byID    = map[string]int{}
		prompts []Prompt
		carry   []byte
		pos     = lineBefore(f, info.Size(), before)
		read    int64
	)
	// older counts what is strictly older than before. The oldest reply may still
	// gain earlier blocks from the next step, so it is not counted until the start.
	older := func() (int, int) {
		r := 0
		for _, x := range replies {
			if x.At.Before(before) {
				r++
			}
		}
		if r > 0 {
			r--
		}
		p := 0
		for _, x := range prompts {
			if x.At.Before(before) {
				p++
			}
		}
		return r, p
	}
	for pos > 0 {
		step := repliesWindow
		if step > pos {
			step = pos
		}
		start := pos - step
		buf := make([]byte, int(step)+len(carry))
		if _, err := f.ReadAt(buf[:step], start); err != nil && !errors.Is(err, io.EOF) {
			return replyPage{}, err
		}
		copy(buf[step:], carry)
		carry = nil
		read += step
		pos = start
		if start > 0 {
			if i := bytes.IndexByte(buf, '\n'); i < 0 {
				carry, buf = buf, nil
			} else {
				carry, buf = buf[:i+1], buf[i+1:]
			}
		}
		s := newTextScan()
		for len(buf) > 0 {
			line := buf
			if i := bytes.IndexByte(buf, '\n'); i >= 0 {
				line, buf = buf[:i], buf[i+1:]
			} else {
				buf = nil
			}
			s.feed(line)
		}
		if !s.firstAt.IsZero() {
			firstAt = s.firstAt
		}
		// This step is older than everything held. A reply already held under the
		// same id takes this step's blocks in front of its own.
		var front []Reply
		var frontIDs []string
		for i, r := range s.out {
			id := s.ids[i]
			if j, ok := byID[id]; ok && id != "" {
				replies[j].Text = r.Text + "\n\n" + replies[j].Text
				replies[j].At = r.At
				continue
			}
			front = append(front, r)
			frontIDs = append(frontIDs, id)
		}
		replies = append(front, replies...)
		ids = append(frontIDs, ids...)
		prompts = append(s.prompts, prompts...)
		byID = map[string]int{}
		for i, id := range ids {
			if id != "" {
				byID[id] = i
			}
		}
		if pos > 0 {
			if read >= repliesReach {
				break
			}
			if r, p := older(); r >= n && p >= n {
				break
			}
		}
	}
	// Short of the start the read is complete only down to its oldest record, and
	// the oldest reply may lack blocks that lie further back. Leave it for the
	// next page, which finds it whole.
	var floor time.Time
	if pos > 0 {
		floor = boundaryAt(f, pos, firstAt)
	}
	var rs []Reply
	for _, r := range replies {
		if r.At.Before(before) {
			rs = append(rs, r)
		}
	}
	var ps []Prompt
	for _, p := range prompts {
		if p.At.Before(before) {
			ps = append(ps, p)
		}
	}
	// Only when another reply is left to set the cut. Dropped alone, it would lie
	// between the cut and the page and no page would carry it.
	if pos > 0 && len(rs) > 1 && !rs[0].At.Equal(rs[1].At) {
		rs = rs[1:]
		floor = laterOf(floor, rs[0].At)
	}
	pg := finishPage(rs, ps, n, floor)
	pg.replies = append([]Reply{}, pg.replies...)
	for i := range pg.replies {
		pg.replies[i].Text, pg.replies[i].Truncated = keepHead(pg.replies[i].Text, replyTextMax)
	}
	pg.prompts = append([]Prompt{}, pg.prompts...)
	return pg, nil
}

// lineBefore is an offset at the start of a line, past which every line is at or
// after before, so a `before` page reads back from there and the reach is counted
// from the page's own place in the file, not from the end. Without it a page deeper
// than the reach could never be answered. Transcript lines are written in time
// order, so it bisects on their timestamps. Anything it cannot read answers the
// file's end, which is always correct and only costs the reach.
func lineBefore(f *os.File, size int64, before time.Time) int64 {
	lo, hi := int64(0), size
	for hi-lo > repliesWindow {
		mid := lo + (hi-lo)/2
		off, at, ok := probeLine(f, mid)
		if !ok || off >= hi {
			return size
		}
		if at.Before(before) {
			lo = off
		} else {
			hi = off
		}
	}
	return hi
}

// probeLine is the start and the timestamp of the first line that starts at or
// after off and carries one.
func probeLine(f *os.File, off int64) (int64, time.Time, bool) {
	buf := make([]byte, 256<<10)
	n, _ := f.ReadAt(buf, off)
	buf = buf[:n]
	i := bytes.IndexByte(buf, '\n')
	if i < 0 {
		return 0, time.Time{}, false
	}
	start := off + int64(i) + 1
	buf = buf[i+1:]
	key := []byte(`"timestamp":"`)
	for {
		j := bytes.IndexByte(buf, '\n')
		if j < 0 {
			return 0, time.Time{}, false
		}
		line := buf[:j]
		if k := bytes.Index(line, key); k >= 0 {
			rest := line[k+len(key):]
			if e := bytes.IndexByte(rest, '"'); e >= 0 {
				if at, err := time.Parse(time.RFC3339Nano, string(rest[:e])); err == nil {
					return start, at, true
				}
			}
		}
		start += int64(j) + 1
		buf = buf[j+1:]
	}
}

// scanReplyText reads transcript lines and returns every main-chain assistant
// reply that has text, in file order, one per message id.
func scanReplyText(r io.Reader) ([]Reply, error) {
	replies, _, err := scanTranscriptText(r)
	return replies, err
}

// scanTranscriptText reads transcript lines and returns every main-chain
// assistant reply that has text, one per message id, and every prompt, both in
// file order.
func scanTranscriptText(r io.Reader) ([]Reply, []Prompt, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	replies, prompts, _, err := scanTranscriptWindow(r)
	return replies, prompts, err
}

// scanTranscriptWindow is scanTranscriptText and the time of the first record
// it read.
func scanTranscriptWindow(r io.Reader) ([]Reply, []Prompt, time.Time, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	s := newTextScan()
	for sc.Scan() {
		s.feed(sc.Bytes())
	}
	if err := sc.Err(); err != nil {
		return nil, nil, time.Time{}, err
	}
	for i := range s.out {
		s.out[i].Text, s.out[i].Truncated = keepHead(s.out[i].Text, replyTextMax)
	}
	return s.out, s.prompts, s.firstAt, nil
}

// textScan accumulates replies and prompts from transcript lines in file order.
// ids holds each reply's message id, parallel to out, for joining across reads.
type textScan struct {
	out     []Reply
	ids     []string
	prompts []Prompt
	index   map[string]int // message id to its place in out
	firstAt time.Time      // the first record with a time, which a read starting mid-file is complete down to
}

func newTextScan() *textScan { return &textScan{index: map[string]int{}} }

// feed takes one line. Reply text is not cut here, the caller cuts it once joined.
func (s *textScan) feed(line []byte) {
	{
		isUser := bytes.Contains(line, []byte(`"user"`))
		if !isUser && !bytes.Contains(line, []byte(`"assistant"`)) {
			return
		}
		var rec struct {
			Type      string `json:"type"`
			Sidechain bool   `json:"isSidechain"`
			Meta      bool   `json:"isMeta"`
			Timestamp string `json:"timestamp"`
			Origin    *struct {
				Kind string `json:"kind"`
			} `json:"origin"`
			Message struct {
				ID      string `json:"id"`
				Content any    `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &rec) != nil {
			return
		}
		at, _ := time.Parse(time.RFC3339Nano, rec.Timestamp)
		if s.firstAt.IsZero() && !at.IsZero() {
			s.firstAt = at.UTC()
		}
		if rec.Sidechain {
			return
		}
		if rec.Type == "user" {
			if rec.Meta || (rec.Origin != nil && rec.Origin.Kind != "" && rec.Origin.Kind != "human") {
				return
			}
			if p, ok := promptOf(rec.Message.Content); ok {
				p.At = at.UTC()
				p.Text, p.Truncated = keepHead(p.Text, replyTextMax)
				s.prompts = append(s.prompts, p)
			}
			return
		}
		if rec.Type != "assistant" {
			return
		}
		text := replyText(rec.Message.Content)
		if text == "" {
			return
		}
		if i, seen := s.index[rec.Message.ID]; seen && rec.Message.ID != "" {
			s.out[i].Text = s.out[i].Text + "\n\n" + text
			return
		}
		s.index[rec.Message.ID] = len(s.out)
		s.out = append(s.out, Reply{At: at.UTC(), Text: text})
		s.ids = append(s.ids, rec.Message.ID)
	}
}

// promptOf is a user line's text and kind, or false for a line that is not
// somebody talking to the card: a tool result, a hook's or a task's
// notification, a command's output.
//
//   - `<command-name>/x</command-name>` and its args is a command, as typed.
//   - text that starts `[atrium] ` is atrium speaking: a peer's message, typed
//     with its banner, or atrium's own wake.
//   - any other text is the operator, wherever it was typed.
func promptOf(content any) (Prompt, bool) {
	var text string
	switch c := content.(type) {
	case string:
		text = c
	case []any:
		var parts []string
		for _, b := range c {
			m, ok := b.(map[string]any)
			if !ok {
				continue
			}
			if m["type"] == "tool_result" {
				return Prompt{}, false
			}
			// A block Claude Code injected beside the operator's text, a
			// <system-reminder> say, is not his words.
			if s, ok := m["text"].(string); ok && m["type"] == "text" {
				if s = strings.TrimSpace(s); s != "" && !strings.HasPrefix(s, "<") {
					parts = append(parts, s)
				}
			}
		}
		text = strings.Join(parts, "\n\n")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Prompt{}, false
	}
	if strings.HasPrefix(text, "<command-name>") {
		cmd := strings.TrimSpace(between(text, "<command-name>", "</command-name>"))
		if args := strings.TrimSpace(between(text, "<command-args>", "</command-args>")); args != "" {
			cmd += " " + args
		}
		if cmd == "" {
			return Prompt{}, false
		}
		return Prompt{Text: cmd, Kind: PromptCommand}, true
	}
	if strings.HasPrefix(text, "<") {
		// <local-command-stdout>, <task-notification>, <system-reminder> and the
		// rest: written by Claude Code, not said by anyone.
		return Prompt{}, false
	}
	if strings.HasPrefix(text, "[atrium] ") {
		return Prompt{Text: text, Kind: PromptPeer}, true
	}
	return Prompt{Text: text, Kind: PromptOperator}, true
}

// between is the text after the first open and before the next close, or "".
func between(s, open, close string) string {
	i := strings.Index(s, open)
	if i < 0 {
		return ""
	}
	s = s[i+len(open):]
	j := strings.Index(s, close)
	if j < 0 {
		return ""
	}
	return s[:j]
}

// replyText keeps an assistant message's text blocks, skipping tool calls and
// thinking, the same rule the session export uses.
func replyText(content any) string {
	blocks, ok := content.([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		m, ok := b.(map[string]any)
		if !ok || m["type"] != "text" {
			continue
		}
		if s, ok := m["text"].(string); ok && strings.TrimSpace(s) != "" {
			parts = append(parts, strings.TrimSpace(s))
		}
	}
	return strings.Join(parts, "\n\n")
}

// keepHead cuts s to at most max bytes on a rune boundary, keeping the start.
func keepHead(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

// keepTail cuts s to at most max bytes on a rune boundary, keeping the end.
func keepTail(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	start := len(s) - max
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:], true
}
