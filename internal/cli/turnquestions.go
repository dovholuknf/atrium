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
	if strings.TrimSpace(path) == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	from := st.Size() - transcriptTail
	if from < 0 {
		from = 0
	}
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(f, transcriptTail))
	if err != nil {
		return ""
	}
	// A read that started mid-line has a partial first line. Dropped, since it
	// cannot parse and is never the last message anyway.
	if from > 0 {
		if i := bytes.IndexByte(raw, '\n'); i >= 0 {
			raw = raw[i+1:]
		}
	}
	lines := bytes.Split(raw, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if t := assistantText(lines[i]); t != "" {
			return t
		}
	}
	return ""
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
