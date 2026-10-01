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

const (
	repliesDefault = 3
	repliesMax     = 10
	// replyTextMax cuts one reply, so a long answer cannot make the page heavy.
	replyTextMax = 16 << 10
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

// repliesFor answers the endpoint for one card.
func (d *Daemon) repliesFor(taskID string, n int) (*RepliesView, error) {
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
				replies, prompts, err := readTranscriptText(path, n)
				if err == nil {
					return &RepliesView{Source: "transcript", Replies: replies, Prompts: prompts}, nil
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
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	repliesCache.mu.Lock()
	c, ok := repliesCache.m[path]
	repliesCache.mu.Unlock()
	if !ok || c.size != info.Size() || !c.mtime.Equal(info.ModTime()) {
		f, err := os.Open(path)
		if err != nil {
			return nil, nil, err
		}
		defer f.Close()
		if info.Size() > transcriptTail {
			if _, err := f.Seek(info.Size()-transcriptTail, io.SeekStart); err != nil {
				return nil, nil, err
			}
		}
		replies, prompts, err := scanTranscriptText(f)
		if err != nil {
			return nil, nil, err
		}
		if len(replies) > repliesMax {
			replies = replies[len(replies)-repliesMax:]
		}
		if len(prompts) > repliesMax {
			prompts = prompts[len(prompts)-repliesMax:]
		}
		c = repliesCached{size: info.Size(), mtime: info.ModTime(), replies: replies, prompts: prompts}
		repliesCache.mu.Lock()
		if len(repliesCache.m) > 400 {
			repliesCache.m = map[string]repliesCached{}
		}
		repliesCache.m[path] = c
		repliesCache.mu.Unlock()
	}
	replies, prompts := c.replies, c.prompts
	if len(replies) > n {
		replies = replies[len(replies)-n:]
	}
	if len(prompts) > n {
		prompts = prompts[len(prompts)-n:]
	}
	return append([]Reply{}, replies...), append([]Prompt{}, prompts...), nil
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
	var (
		out     []Reply
		prompts []Prompt
		index   = map[string]int{} // message id to its place in out
	)
	for sc.Scan() {
		line := sc.Bytes()
		isUser := bytes.Contains(line, []byte(`"user"`))
		if !isUser && !bytes.Contains(line, []byte(`"assistant"`)) {
			continue
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
		if json.Unmarshal(line, &rec) != nil || rec.Sidechain {
			continue
		}
		at, _ := time.Parse(time.RFC3339Nano, rec.Timestamp)
		if rec.Type == "user" {
			if rec.Meta || (rec.Origin != nil && rec.Origin.Kind != "" && rec.Origin.Kind != "human") {
				continue
			}
			if p, ok := promptOf(rec.Message.Content); ok {
				p.At = at.UTC()
				p.Text, p.Truncated = keepHead(p.Text, replyTextMax)
				prompts = append(prompts, p)
			}
			continue
		}
		if rec.Type != "assistant" {
			continue
		}
		text := replyText(rec.Message.Content)
		if text == "" {
			continue
		}
		if i, seen := index[rec.Message.ID]; seen && rec.Message.ID != "" {
			out[i].Text = out[i].Text + "\n\n" + text
			continue
		}
		index[rec.Message.ID] = len(out)
		out = append(out, Reply{At: at.UTC(), Text: text})
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}
	for i := range out {
		out[i].Text, out[i].Truncated = keepHead(out[i].Text, replyTextMax)
	}
	return out, prompts, nil
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
			if s, ok := m["text"].(string); ok && m["type"] == "text" && strings.TrimSpace(s) != "" {
				parts = append(parts, strings.TrimSpace(s))
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
