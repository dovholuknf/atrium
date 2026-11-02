package promptline

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

// Prompt kinds. See PromptOf.
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

// QueuedPromptOf is the prompt in a line Claude Code wrote for a message typed while a turn was running:
// {"type":"attachment","attachment":{"type":"queued_command","commandMode":"prompt","prompt":...}}. It is a
// user line in all but its shape, and the reply to it is a new turn. Only the main chain's count.
func QueuedPromptOf(line []byte) (Prompt, bool) {
	if !bytes.Contains(line, []byte(`queued_command`)) {
		return Prompt{}, false
	}
	var rec struct {
		Type       string `json:"type"`
		Sidechain  bool   `json:"isSidechain"`
		Timestamp  string `json:"timestamp"`
		Attachment struct {
			Type        string `json:"type"`
			CommandMode string `json:"commandMode"`
			Prompt      any    `json:"prompt"`
		} `json:"attachment"`
	}
	if json.Unmarshal(line, &rec) != nil || rec.Type != "attachment" || rec.Sidechain ||
		rec.Attachment.Type != "queued_command" || (rec.Attachment.CommandMode != "" && rec.Attachment.CommandMode != "prompt") {
		return Prompt{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
	if err != nil {
		return Prompt{}, false
	}
	p, ok := PromptOf(rec.Attachment.Prompt)
	if !ok {
		return Prompt{}, false
	}
	p.At = at.UTC()
	return p, true
}

// PromptOf is a user line's text and kind, or false for a line that is not
// somebody talking to the card: a tool result, a hook's or a task's
// notification, a command's output.
//
//   - `<command-name>/x</command-name>` and its args is a command, as typed.
//   - text that starts `[atrium] ` is atrium speaking: a peer's message, typed
//     with its banner, or atrium's own wake.
//   - any other text is the operator, wherever it was typed.
func PromptOf(content any) (Prompt, bool) {
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
