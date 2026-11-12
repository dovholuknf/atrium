package api

import (
	"bufio"
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

func walkStr(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return strings.TrimSpace(v)
}

// walkMerge folds the agent's lines into a saved walk and reports how many comments and replies were new. A line
// is a new comment ({file, line, text, scope?, summary?}) or a reply ({reply: <comment id>, text}). The nth
// {reply: "whole", text} answers the nth question of the whole-walk thread. An id is a hash of the line's content,
// so reading the file again adds nothing, and an id in `gone` is a comment the walker deleted.
func walkMerge(state map[string]any, lines [][]byte) int {
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
			if !have {
				c["turns"] = append(turns, map[string]any{"who": "agent", "text": text, "id": id, "at": time.Now().UnixMilli()})
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
		id := walkHash("a", file, strconv.Itoa(line), text)
		if byID[id] != nil || isGone(id) {
			continue
		}
		c := map[string]any{"id": id, "file": file, "line": line, "who": "agent", "text": text, "turns": []any{}, "post": nil, "at": time.Now().UnixMilli()}
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
	if walkMerge(state, lines) > 0 {
		out, _ := json.Marshal(state)
		if err := s.st.SetSetting(walkStateKey(task.ID), string(out)); err != nil {
			s.fail(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": state})
}
