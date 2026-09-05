package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/safepath"
)

// The conversations a directory already has, so resuming can offer a choice.
//
// A card carries ONE resume id, the last session atrium saw on it. That is the
// right default and it is not the whole truth: a directory accumulates
// conversations, and the one worth picking up is often not the most recent.
// `claude --resume` answers this in a terminal and there was no answer here.
//
// **This reads Claude Code's own transcripts and nothing else writes them.**
// Held in `~/.claude/projects/<encoded cwd>/<session id>.jsonl`, one JSON
// object per line. Atrium reads the first handful of lines and stops: a
// transcript reaches ninety megabytes in a working day and the two facts worth
// having are both at the top.
//
// Harness-specific on purpose, and the only place in atrium that is. A runner
// row cannot express "and here is where your transcripts live" yet, and
// inventing that schema for one harness would be guessing at the shape of the
// second. When codex or another runner needs this, the shape it needs will be
// visible; until then this answers empty for them, which reads correctly as
// "no conversations to choose from".

// titleScanLines bounds how far into a transcript the title is looked for.
//
// `ai-title` lands around line seventeen in practice, after the mode records
// and the first exchange. Fifty is generous and costs a few kilobytes against
// a file that can be a hundred megabytes.
const titleScanLines = 50

// sessionView is one resumable conversation.
type sessionView struct {
	ID string `json:"id"`
	// Title is what Claude Code called it, or the first thing said to it.
	Title string `json:"title"`
	// At is the last write, which is when it was last talked to.
	At    string `json:"at"`
	Bytes int64  `json:"bytes"`
	// Current marks the one the card would resume without being asked, so the
	// board can say which is the default rather than making somebody work it
	// out from timestamps.
	Current bool `json:"current,omitempty"`
}

// projectDirFor is where Claude Code keeps a directory's transcripts.
//
// The encoding is its own: the drive colon and every separator become a dash,
// so `D:\git\github\dovholuknf\atrium` is `D--git-github-dovholuknf-atrium`.
// Read off the directory rather than documented anywhere, so it is checked by
// existing rather than trusted.
func projectDirFor(cwd string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	enc := strings.NewReplacer(`:`, `-`, `\`, `-`, `/`, `-`).Replace(strings.TrimSpace(cwd))
	return filepath.Join(home, ".claude", "projects", enc), nil
}

// LatestSession is the most recently written conversation in a directory, or
// empty when there is none.
//
// What a fixture actually wants. A fixture says "give me back the terminal I
// had", and anchoring that to a RECORDED id is wrong twice: the id goes stale
// the moment anything else starts a session in that directory, and it points at
// nothing at all once that transcript is deleted. Both fail the same silent
// way, a fresh conversation with nothing saying why.
//
// Exported because the daemon starts fixtures and this reads Claude Code's
// files, which is knowledge that belongs on the side that already has it.
func LatestSession(cwd string) string {
	dir, err := projectDirFor(cwd)
	if err != nil {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best, bestAt := "", time.Time{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(bestAt) {
			best, bestAt = strings.TrimSuffix(e.Name(), ".jsonl"), info.ModTime()
		}
	}
	return best
}

// SessionExists reports whether a conversation is still on disk.
//
// Asking to resume one that has been deleted is not an error anywhere: the
// runner starts fresh and says nothing, the card keeps pointing at a file that
// is gone, and every restart repeats it.
func SessionExists(cwd, id string) bool {
	if strings.TrimSpace(id) == "" {
		return false
	}
	dir, err := projectDirFor(cwd)
	if err != nil {
		return false
	}
	full, err := safepath.Contained(dir, filepath.Join(dir, id+".jsonl"))
	if err != nil {
		return false
	}
	_, err = os.Stat(full)
	return err == nil
}

func (s *Server) taskSessions(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if strings.TrimSpace(task.Worktree) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("this card has no directory"))
		return
	}

	dir, err := projectDirFor(task.Worktree)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": []sessionView{}})
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		// No transcripts is the ordinary answer for a directory nothing has
		// run in, and for every harness that is not Claude Code. Not an error.
		writeJSON(w, http.StatusOK, map[string]any{"sessions": []sessionView{}})
		return
	}

	out := []sessionView{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		out = append(out, sessionView{
			ID:      id,
			Title:   titleOf(filepath.Join(dir, e.Name()), id),
			At:      info.ModTime().UTC().Format(time.RFC3339),
			Bytes:   info.Size(),
			Current: id == task.ResumeID,
		})
	}
	// Newest first: the conversation you want is usually one of the last few,
	// and a list ordered by name is ordered by nothing.
	sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

// titleOf is what to call a transcript.
//
// Claude Code writes an `ai-title` record once it has enough to name the
// conversation. Before that there is only what was said first, which is worse
// but is better than a uuid.
func titleOf(path, id string) string {
	f, err := os.Open(path)
	if err != nil {
		return id
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	// A single line can be a whole pasted file, and the default limit would
	// stop the scan dead at the first one.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	first := ""
	for i := 0; i < titleScanLines && sc.Scan(); i++ {
		var rec struct {
			Type    string `json:"type"`
			AITitle string `json:"aiTitle"`
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		if rec.Type == "ai-title" && strings.TrimSpace(rec.AITitle) != "" {
			return rec.AITitle
		}
		if first == "" && rec.Type == "user" {
			if text, ok := rec.Message.Content.(string); ok {
				first = firstLineOf(text)
			}
		}
	}
	if first != "" {
		return first
	}
	return id
}

// firstLineOf reduces a prompt to something that fits on a row.
//
// Slash commands and the caveat block a local command leaves behind are
// skipped, because a list where three rows all say `/exit` names nothing.
func firstLineOf(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "<") || strings.HasPrefix(line, "/") {
			continue
		}
		if len(line) > 72 {
			line = line[:72] + "…"
		}
		return line
	}
	return ""
}

// forgetSession deletes one transcript.
//
// Resolved through `internal/safepath` against the project directory, so a
// crafted id cannot name a file outside it. The id comes from the board, which
// got it from the listing above, and that is exactly the kind of round trip
// worth not trusting.
func (s *Server) forgetSession(w http.ResponseWriter, r *http.Request) {
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
	if err := os.Remove(full); err != nil {
		writeErr(w, http.StatusNotFound, errors.New("no such conversation"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "forgot": want})
}
