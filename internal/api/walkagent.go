package api

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/base/gitexec"
)

// walkAgentFile is where the walk agent leaves comments and replies, one JSON object per line. It sits beside PR.md
// in the worktree because a room never lets the agent call out, and the board reads it through walkAgentSync.
const walkAgentFile = ".atrium/walk-agent.jsonl"

// walkMu serialises every read-modify-write of a saved walk, so a sync never loses a save that landed beside it.
var walkMu sync.Mutex

func walkHash(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + hex.EncodeToString(sum[:4])
}

// walkLineMap follows a line of the old file through a unified diff of that file to its line in the new one. It
// answers false when the diff removes or rewrites the line, so the caller never pins the comment to another line.
func walkLineMap(diff string, line int) (int, bool) {
	delta, old, cur, in := 0, 0, 0, false
	for _, l := range strings.Split(diff, "\n") {
		if strings.HasPrefix(l, "@@") {
			if in {
				delta = cur - old
			}
			var a, b, c, d int
			f := strings.Fields(l)
			if len(f) < 3 {
				continue
			}
			a, b = walkRange(f[1])
			c, d = walkRange(f[2])
			if b == 0 {
				a++
			}
			if d == 0 {
				c++
			}
			if line < a {
				return line + delta, true
			}
			old, cur, in = a, c, true
			continue
		}
		if !in || l == "" {
			continue
		}
		switch l[0] {
		case ' ':
			if old == line {
				return cur, true
			}
			old++
			cur++
		case '-':
			if old == line {
				return 0, false
			}
			old++
		case '+':
			cur++
		}
	}
	if in {
		delta = cur - old
	}
	return line + delta, true
}

// walkRange reads "-12,3" or "+7" as start and count.
func walkRange(s string) (int, int) {
	a, b, _ := strings.Cut(strings.TrimLeft(s, "-+"), ",")
	start, _ := strconv.Atoi(a)
	n := 1
	if b != "" {
		n, _ = strconv.Atoi(b)
	}
	return start, n
}

func walkStr(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return strings.TrimSpace(v)
}

// walkMerge folds the agent's lines into a saved walk and reports how many comments and replies were new. A line
// is a new comment ({file, line, text, scope?, summary?}) or a reply ({reply: <comment id>, text, who?: "reviewer"}).
// The nth {reply: "whole", text} answers the nth question of the whole-walk thread. An id is a hash of the line's
// content, so reading the file again adds nothing, and an id in `gone` is a comment the walker deleted.
func walkMerge(state map[string]any, lines [][]byte, head string) int {
	comments, _ := state["comments"].([]any)
	gone, _ := state["gone"].([]any)
	byID := map[string]map[string]any{}
	for _, c := range comments {
		if m, ok := c.(map[string]any); ok {
			byID[walkStr(m, "id")] = m
		}
	}
	isGone := func(id string) bool {
		for _, g := range gone {
			if g == id {
				return true
			}
		}
		return false
	}
	added, wholes := 0, 0
	for _, raw := range lines {
		var in map[string]any
		if json.Unmarshal(raw, &in) != nil {
			continue
		}
		text := walkStr(in, "text")
		if text == "" {
			continue
		}
		if walkStr(in, "reply") == "whole" {
			thread, _ := state["thread"].([]any)
			if wholes < len(thread) {
				if m, ok := thread[wholes].(map[string]any); ok && walkStr(m, "a") == "" {
					m["a"] = text
					added++
				}
			}
			wholes++
			continue
		}
		if to := walkStr(in, "reply"); to != "" {
			c := byID[to]
			if c == nil {
				continue
			}
			id := walkHash("t", to, text)
			turns, _ := c["turns"].([]any)
			have := false
			for _, t := range turns {
				if m, ok := t.(map[string]any); ok && m["id"] == id {
					have = true
				}
			}
			who := "agent"
			if walkStr(in, "who") == "reviewer" {
				who = "reviewer"
			}
			if !have {
				c["turns"] = append(turns, map[string]any{"who": who, "text": text, "id": id, "at": time.Now().UnixMilli()})
				added++
			}
			continue
		}
		file, line := walkStr(in, "file"), 0
		if f, ok := in["line"].(float64); ok {
			line = int(f)
		}
		if file == "" || line < 1 {
			continue
		}
		sha := walkStr(in, "sha")
		if sha == "" {
			sha = head
		}
		id := walkHash("a", file, strconv.Itoa(line), text)
		if byID[id] != nil || isGone(id) {
			continue
		}
		c := map[string]any{"id": id, "file": file, "line": line, "who": "agent", "text": text, "turns": []any{}, "post": nil, "at": time.Now().UnixMilli()}
		if sha != "" {
			c["sha"] = sha
		}
		for _, k := range []string{"scope", "summary"} {
			if v := walkStr(in, k); v != "" {
				c[k] = v
			}
		}
		comments = append(comments, c)
		byID[id] = c
		added++
	}
	if comments != nil {
		state["comments"] = comments
	}
	return added
}

// POST /v1/tasks/{id}/walk/agent reads the agent's file in the card's worktree into the saved walk and answers with
// the walk. A card with no file answers its walk unchanged.
func (s *Server) walkAgentSync(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	walkMu.Lock()
	defer walkMu.Unlock()
	raw, err := s.st.Setting(walkStateKey(task.ID))
	if err != nil {
		s.fail(w, err)
		return
	}
	state := map[string]any{}
	if raw != "" && json.Unmarshal([]byte(raw), &state) != nil {
		writeErr(w, http.StatusInternalServerError, errors.New("the saved walk is not an object"))
		return
	}
	var lines [][]byte
	if task.Worktree != "" {
		if f, err := os.Open(filepath.Join(filepath.FromSlash(task.Worktree), filepath.FromSlash(walkAgentFile))); err == nil {
			defer f.Close()
			sc := bufio.NewScanner(http.MaxBytesReader(w, f, maxWalkStateBytes))
			sc.Buffer(nil, 1<<16)
			for sc.Scan() {
				lines = append(lines, append([]byte(nil), sc.Bytes()...))
			}
		}
	}
	head := ""
	if task.Worktree != "" {
		if out, err := gitOut(r.Context(), task.Worktree, nil, gitexec.RevParse{Rev: "HEAD"}); err == nil {
			head = strings.TrimSpace(string(out))
		}
	}
	if walkMerge(state, lines, head) > 0 {
		out, _ := json.Marshal(state)
		if err := s.st.SetSetting(walkStateKey(task.ID), string(out)); err != nil {
			s.fail(w, err)
			return
		}
	}
	s.walkRemap(r.Context(), task.Worktree, head, state)
	writeJSON(w, http.StatusOK, map[string]any{"state": state, "head": head})
}

// walkRemap sets `now` on each comment that has a sha: its line at head, or 0 when that line changed or the sha is
// unknown. It is derived on every read and is not saved, so a comment keeps the line it was made on.
func (s *Server) walkRemap(ctx context.Context, dir, head string, state map[string]any) {
	comments, _ := state["comments"].([]any)
	diffs := map[string]string{}
	for _, raw := range comments {
		c, ok := raw.(map[string]any)
		sha := ""
		if ok {
			sha = walkStr(c, "sha")
		}
		line, _ := c["line"].(float64)
		if sha == "" || head == "" || dir == "" {
			continue
		}
		file := walkStr(c, "file")
		now := int(line)
		if sha != head {
			key := sha + "\x00" + file
			d, seen := diffs[key]
			if !seen {
				out, err := gitOut(ctx, dir, nil, gitexec.Diff{Revs: []string{sha, head}, Paths: []string{file}, NoColor: true, NoExtDiff: true, NoRenames: true})
				d = string(out)
				if err != nil {
					d = "!"
				}
				diffs[key] = d
			}
			now = 0
			if d != "!" {
				if n, ok := walkLineMap(d, int(line)); ok {
					now = n
				}
			}
		}
		c["now"] = now
	}
}
