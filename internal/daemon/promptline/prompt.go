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

const hookBanner = "Message from the human, sent through atrium"

// HookPromptOf is the operator's message in a line Claude Code wrote when a hook carried it: a Stop hook's
// `stop_hook_summary` (hookErrors) or a permission hook's error `tool_result`. Both open with hookBanner and end
// the banner at the first blank line. A batch mixed with a peer's says so in its banner and is left out.
func HookPromptOf(line []byte) (Prompt, bool) {
	if !bytes.Contains(line, []byte(hookBanner)) {
		return Prompt{}, false
	}
	var rec struct {
		Type      string   `json:"type"`
		Subtype   string   `json:"subtype"`
		Sidechain bool     `json:"isSidechain"`
		Timestamp string   `json:"timestamp"`
		Errors    []string `json:"hookErrors"`
		Message   struct {
			Content any `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &rec) != nil || rec.Sidechain {
		return Prompt{}, false
	}
	var texts []string
	switch {
	case rec.Type == "system" && rec.Subtype == "stop_hook_summary":
		texts = rec.Errors
	case rec.Type == "user":
		blocks, _ := rec.Message.Content.([]any)
		for _, b := range blocks {
			m, _ := b.(map[string]any)
			if m["type"] != "tool_result" || m["is_error"] != true {
				continue
			}
			switch c := m["content"].(type) {
			case string:
				texts = append(texts, c)
			case []any:
				for _, p := range c {
					if pm, ok := p.(map[string]any); ok {
						if s, ok := pm["text"].(string); ok {
							texts = append(texts, s)
						}
					}
				}
			}
		}
	}
	at, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
	if err != nil {
		return Prompt{}, false
	}
	for _, t := range texts {
		i := strings.Index(t, hookBanner)
		if i < 0 {
			continue
		}
		rest := t[i+len(hookBanner):]
		j := strings.Index(rest, "\n\n")
		if j < 0 || !strings.HasPrefix(rest, ":") && !strings.HasPrefix(rest, ".") {
			continue
		}
		if text := strings.TrimSpace(rest[j+2:]); text != "" {
			return Prompt{At: at.UTC(), Text: text, Kind: PromptOperator}, true
		}
	}
	return Prompt{}, false
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
