package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

// The Open Questions a turn ended on, read by the Stop hook and sent as a list.
//
// ATRIUM STILL DOES NOT RECORD WHAT A SESSION SAID. The hook reads the turn's
// last message, keeps only the numbered lines of its last `Open Questions:`
// block, and sends those. The message and the transcript never leave this
// process. See docs/seen-design.md.
//
// THE HOOK POSTURE HOLDS. Every failure here is "the text is not known", which
// leaves the card's questions as they were. Nothing here can fail a turn: the
// read is bounded in size, and a transcript that is missing, huge, or not JSON
// is the same answer as no transcript at all.

// transcriptTail is how much of the transcript is read, from the end. The last
// assistant message is at the end, and a transcript can be tens of megabytes.
const transcriptTail = 256 << 10

// turnQuestionMax and turnQuestionLen bound what is sent. The daemon bounds it
// again on the way in, and these only keep the post small.
const (
	turnQuestionMax = 10
	turnQuestionLen = 500
)

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

// The heading, read leniently: `Open Questions:`, bold, or a markdown heading.
var (
	oqHeading = regexp.MustCompile(`(?i)^\s*(#{1,6}\s*)?(\*\*|__)?\s*open questions\s*:?\s*(\*\*|__)?\s*:?\s*$`)
	oqItem    = regexp.MustCompile(`^\s{0,3}(\d{1,2})[.)]\s+(.*)$`)
	oqRule    = regexp.MustCompile(`^\s*(-{3,}|\*{3,}|_{3,})\s*$`)
)

// openQuestions extracts the numbered items of the LAST Open Questions block.
//
// The last, because a message that quotes an earlier block and then asks its
// own means the second. An indented line continues the item above it. The
// first non-blank line after an item that is neither an item nor indented ends
// the block.
func openQuestions(text string) (list []string, block bool) {
	lines := []string{}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		lines = append(lines, strings.TrimRight(sc.Text(), "\r"))
	}
	start := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if oqHeading.MatchString(lines[i]) {
			start = i
			break
		}
	}
	if start < 0 {
		return nil, false
	}
	var cur *strings.Builder
	flush := func() {
		if cur == nil {
			return
		}
		q := strings.TrimSpace(cur.String())
		if q != "" && len(list) < turnQuestionMax {
			if len(q) > turnQuestionLen {
				q = q[:turnQuestionLen]
				// Not in the middle of a character.
				for !utf8.ValidString(q) {
					q = q[:len(q)-1]
				}
			}
			list = append(list, q)
		}
		cur = nil
	}
	for _, l := range lines[start+1:] {
		switch {
		case strings.TrimSpace(l) == "", oqRule.MatchString(l):
			continue
		case oqItem.MatchString(l):
			flush()
			m := oqItem.FindStringSubmatch(l)
			cur = &strings.Builder{}
			cur.WriteString(strings.TrimSpace(m[2]))
		case cur != nil && (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")):
			cur.WriteString(" ")
			cur.WriteString(strings.TrimSpace(l))
		default:
			// Prose after the list, or prose where a list should be.
			flush()
			return list, true
		}
	}
	flush()
	return list, true
}
