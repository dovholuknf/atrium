package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// The Open Questions a turn ended on, read by the Stop hook and sent as a list.
//
// ATRIUM STILL DOES NOT RECORD WHAT A SESSION SAID. The hook reads the turn's
// last message, keeps only the numbered lines of its last `Open Questions:`
// block, and sends those. The message and the transcript never leave this
// process. See docs/runtime/seen-design.md.
//
// THE HOOK POSTURE HOLDS. Every failure here is "the text is not known", which
// leaves the card's questions as they were. Nothing here can fail a turn: the
// read is bounded in size, and a transcript that is missing, huge, or not JSON
// is the same answer as no transcript at all.

// transcriptTail is how much of the transcript is read, from the end. The last
// assistant message is at the end, and a transcript can be tens of megabytes.
const transcriptTail = 256 << 10

// turnQuestions reads the turn's last message and extracts its questions.
func turnQuestions(in turnInput) (list []string, block, known bool) {
	text := in.LastAssistantMessage
	if strings.TrimSpace(text) == "" {
		text = lastAssistantText(in.TranscriptPath)
	}
	if strings.TrimSpace(text) == "" {
		return nil, false, false
	}
	list, block = openQuestions(text)
	return list, block, true
}

// lastAssistantText returns the text of the last assistant entry in a Claude
// Code transcript, or "" when there is none to be had.
func lastAssistantText(path string) string {
	raw := transcriptTailBytes(path)
	if raw == nil {
		return ""
	}
	lines := bytes.Split(raw, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if t := assistantText(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

// transcriptTailBytes is the last transcriptTail bytes of a transcript, whole lines only, or nil.
func transcriptTailBytes(path string) []byte {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	from := st.Size() - transcriptTail
	if from < 0 {
		from = 0
	}
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(f, transcriptTail))
	if err != nil {
		return nil
	}
	// A read that started mid-line has a partial first line. Dropped, since it
	// cannot parse and is never the last message anyway.
	if from > 0 {
		if i := bytes.IndexByte(raw, '\n'); i >= 0 {
			raw = raw[i+1:]
		}
	}
	return raw
}

// ownTurn reports whether the turn that just ended was started by Claude Code and not by anyone: the last real
// prompt in the transcript is a `<task-notification>` or another tag Claude Code writes, the same line
// internal/daemon/replies.go draws. A slash command, a peer's say and a human's text are not it. Anything
// unreadable is false, which is the old behaviour: a ready notice.
func ownTurn(path string) bool {
	raw := transcriptTailBytes(path)
	if raw == nil {
		return false
	}
	lines := bytes.Split(raw, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if own, found := promptIsOwn(lines[i]); found {
			return own
		}
	}
	return false
}

// promptIsOwn reads one transcript line. found is false for anything that is not a user prompt, a tool result
// included, since a tool result is the middle of a turn and not its start.
func promptIsOwn(line []byte) (own, found bool) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' {
		return false, false
	}
	var e struct {
		Type    string `json:"type"`
		Message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &e) != nil || (e.Type != "user" && e.Message.Role != "user") {
		return false, false
	}
	var text string
	if json.Unmarshal(e.Message.Content, &text) != nil {
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(e.Message.Content, &blocks) != nil {
			return false, false
		}
		// A tag block beside the human's text is Claude Code's, not his words: only the other blocks decide.
		var parts []string
		var tagged string
		for _, b := range blocks {
			if b.Type == "tool_result" {
				return false, false
			}
			if b.Type == "text" {
				if s := strings.TrimSpace(b.Text); strings.HasPrefix(s, "<") {
					tagged = s
				} else if s != "" {
					parts = append(parts, s)
				}
			}
		}
		text = strings.Join(parts, "\n")
		if text == "" {
			text = tagged
		}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return false, false
	}
	return strings.HasPrefix(text, "<") && !strings.HasPrefix(text, "<command-name>"), true
}

// assistantText is the joined text blocks of one transcript line, when it is
// an assistant entry. Tool calls and thinking are skipped.
func assistantText(line []byte) string {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' {
		return ""
	}
	var e struct {
		Type    string `json:"type"`
		Message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &e) != nil {
		return ""
	}
	if e.Type != "assistant" && e.Message.Role != "assistant" {
		return ""
	}
	// Content is a string in some writers and a list of blocks in Claude Code.
	var s string
	if json.Unmarshal(e.Message.Content, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(e.Message.Content, &blocks) != nil {
		return ""
	}
	var b strings.Builder
	for _, bl := range blocks {
		if bl.Type == "text" && bl.Text != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(bl.Text)
		}
	}
	return b.String()
}

// openQuestions is store.OpenQuestions: the daemon reads a say's questions the same way.
func openQuestions(text string) ([]string, bool) { return store.OpenQuestions(text) }

const (
	turnQuestionMax = 10
	turnQuestionLen = 500
)
