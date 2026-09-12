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

// A conversation, written out to a file.
//
// Two shapes, and the second is the point: the transcript exactly as Claude
// Code wrote it, and a reduced one that is only what the operator said and
// what the agent said back. A conversation is the only record of why the code
// looks like it does, and today it lives in one place, in a format nobody
// reads, that forgetting the session deletes.
//
// THE SECOND PLACE THE "FILES NEVER LEAVE A CARD" RULE BENDS, after
// `forgetSession`, and for the same reason: transcripts are not inside a card,
// they are under the user's home directory, and the board is the only thing
// that knows which card they belong to. So the id is not trusted. It comes
// from the board, which got it from the listing, and it is resolved through
// `internal/safepath` against the project directory like every path that makes
// that round trip.
//
// IT STREAMS. A transcript reaches ninety megabytes in a working day, which is
// why `sessions.go` reads the head and stops, and one line can be an entire
// pasted file, which is why the scanner buffer goes to four megabytes here as
// it does there. Nothing holds the whole file, in either mode.

// exportSession writes one transcript to the response.
//
// `?mode=raw` is the file itself. Anything else is the reduced markdown, which
// is the one somebody would keep, paste into a ticket, or read a week later.
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

// boilDown writes the conversation and drops everything else.
//
// The filter is a CONTENT-SHAPE test rather than a role test, because a record
// of type `user` is two different things: a prompt somebody typed, where
// `message.content` is a string, and the result of a tool call, where it is an
// array of blocks. `titleOf` already leans on exactly that. An assistant
// record is per-block instead, since one of them can carry text, a tool call
// and thinking together.
//
// A SUBAGENT'S WHOLE CONVERSATION IS IN THE SAME FILE, written inline as
// sidechain records. Left in, another agent's exchange outweighs the
// operator's own turns and reads as somebody else talking, so it is dropped
// and counted, and the count is printed at the end rather than discovered.
//
// What it does NOT do is follow a chain. Resuming makes a new session id, so
// one conversation can be several files, and this exports the one that was
// asked for. The compaction marker below is printed rather than hidden, which
// at least says where the rest of it went.
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

// injectedTags are the things that arrive as user content without anybody
// typing them: reminders the harness adds, the block a slash command leaves
// behind, and hook output.
//
// Named rather than matched as "any line opening with a bracket", which is
// what `firstLineOf` does with one line and cannot do with a whole turn: a
// pasted diff or a line of HTML would take the rest of the message with it.
var injectedTags = []string{
	"system-reminder", "command-name", "command-message", "command-args",
	"local-command-stdout", "local-command-stderr", "user-prompt-submit-hook",
}

// spoken is what is left of a user turn once the injected blocks are out.
//
// Empty means nothing was typed, so there is no turn to print. A turn that is
// only a slash command counts as nothing for the same reason `firstLineOf`
// skips them: the interesting part is what the command did, not that it ran.
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

// assistantText keeps the text blocks and drops the rest.
//
// Tool calls and thinking are the traffic this export exists to boil out, and
// they are siblings of the text in the same record rather than records of
// their own.
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
