package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/safepath"
)

// Export a Claude Code transcript as raw JSONL or conversation text.
// Transcripts live under the user's home, outside the card directory, so resolve
// the requested session id with safepath against its project directory.
// Stream both formats; allow lines up to four megabytes for pasted content.

// exportSession streams raw JSONL for ?mode=raw, or reduced Markdown otherwise.
func (s *Server) exportSession(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	dir, err := projectDirFor(task.Worktree)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	want := strings.TrimSpace(r.PathValue("session"))
	if want == "" {
		writeErr(w, http.StatusBadRequest, errors.New("no session"))
		return
	}
	full, err := safepath.Contained(dir, filepath.Join(dir, want+".jsonl"))
	if err != nil {
		writeErr(w, http.StatusForbidden, safepath.ErrOutside)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		writeErr(w, http.StatusNotFound, errors.New("no such conversation"))
		return
	}
	defer f.Close()

	name := safepath.SafeName(want)
	// ALWAYS an attachment, and never a type a browser renders. A transcript
	// is full of text somebody else wrote, and the board's own origin holds
	// every card and every setting.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Query().Get("mode") == "raw" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.jsonl"`)
		io.Copy(w, f)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.md"`)
	boilDown(w, f, titleOf(full, want), want)
}

// boilDown keeps user prompts and assistant text. Skip user tool results
// with array content and filter assistant records by block type.
// Skip sidechain conversations and report their count. Export only the requested
// file, without following resume chains, and keep compaction markers visible.
func boilDown(w io.Writer, r io.Reader, title, id string) {
	fmt.Fprintf(w, "# %s\n\n`%s`\n\n", title, id)

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	sidechains := 0
	for sc.Scan() {
		var rec struct {
			Type        string `json:"type"`
			IsSidechain bool   `json:"isSidechain"`
			IsCompact   bool   `json:"isCompactSummary"`
			Message     struct {
				Content any `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		if rec.IsSidechain {
			sidechains++
			continue
		}
		switch rec.Type {
		case "user":
			text, ok := rec.Message.Content.(string)
			if !ok {
				// An array here is a tool result, which is the traffic.
				continue
			}
			if said := spoken(text); said != "" {
				fmt.Fprintf(w, "## you\n\n%s\n\n", said)
			}
		case "assistant":
			said := assistantText(rec.Message.Content)
			if said == "" {
				continue
			}
			if rec.IsCompact {
				fmt.Fprintf(w, "---\n\n## compacted here\n\n%s\n\n", said)
				continue
			}
			fmt.Fprintf(w, "## claude\n\n%s\n\n", said)
		}
	}
	if sidechains > 0 {
		fmt.Fprintf(w, "---\n\n_%d subagent records left out._\n", sidechains)
	}
}

// injectedTags names harness reminders, slash-command output, and hook blocks.
// Match known tags so pasted HTML or diffs are not mistaken for injected text.
var injectedTags = []string{
	"system-reminder", "command-name", "command-message", "command-args",
	"local-command-stdout", "local-command-stderr", "user-prompt-submit-hook",
}

// spoken removes injected blocks from a user turn. Empty turns and standalone
// slash commands are omitted from the export.
func spoken(s string) string {
	var out []string
	closing := ""
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if closing != "" {
			if strings.HasPrefix(t, closing) {
				closing = ""
			}
			continue
		}
		if tag := injectedTag(t); tag != "" {
			if !strings.Contains(t, "</"+tag) {
				closing = "</" + tag
			}
			continue
		}
		out = append(out, line)
	}
	said := strings.TrimSpace(strings.Join(out, "\n"))
	if strings.HasPrefix(said, "/") {
		return ""
	}
	return said
}

func injectedTag(line string) string {
	for _, name := range injectedTags {
		if strings.HasPrefix(line, "<"+name) {
			return name
		}
	}
	return ""
}

// assistantText keeps text blocks and skips tool calls and thinking blocks.
func assistantText(content any) string {
	blocks, ok := content.([]any)
	if !ok {
		return ""
	}
	var out []string
	for _, b := range blocks {
		m, ok := b.(map[string]any)
		if !ok || m["type"] != "text" {
			continue
		}
		if s, ok := m["text"].(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return strings.Join(out, "\n\n")
}
