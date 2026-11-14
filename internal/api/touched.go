package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The folders a card's session has touched, read from its latest transcript. They never widen what the board may
// open. They only let the probe say that a relative path the agent printed is in a folder the session works in,
// and that the folder is not an open path, so the board can tell the operator why the path is not a link.

// touchedTail is how much of the end of a transcript is read. The recent calls are the ones a path on screen came from.
const touchedTail = 8 << 20

const touchedFresh = 20 * time.Second

var touchedCache = struct {
	mu sync.Mutex
	m  map[string]touchedEntry
}{m: map[string]touchedEntry{}}

type touchedEntry struct {
	at   time.Time
	dirs []string
}

// touchedDirs is every folder from a touched file's own up to its git root, from memory for touchedFresh.
func touchedDirs(worktree string) []string {
	touchedCache.mu.Lock()
	e, ok := touchedCache.m[worktree]
	touchedCache.mu.Unlock()
	if ok && time.Since(e.at) < touchedFresh {
		return e.dirs
	}
	dirs := scanTouched(worktree)
	touchedCache.mu.Lock()
	touchedCache.m[worktree] = touchedEntry{time.Now(), dirs}
	touchedCache.mu.Unlock()
	return dirs
}

func scanTouched(worktree string) []string {
	id := LatestSession(worktree)
	dir, err := projectDirFor(worktree)
	if id == "" || err != nil {
		return nil
	}
	f, err := os.Open(filepath.Join(dir, id+".jsonl"))
	if err != nil {
		return nil
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() > touchedTail {
		f.Seek(info.Size()-touchedTail, io.SeekStart)
	}
	var out []string
	seen := map[string]bool{}
	br := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		if bytes.Contains(line, []byte(`"tool_use"`)) {
			for _, p := range toolPaths(line) {
				p = filepath.FromSlash(p)
				if !filepath.IsAbs(p) {
					continue
				}
				for _, d := range repoDirs(filepath.Dir(p)) {
					if !seen[d] {
						seen[d] = true
						out = append(out, d)
					}
				}
			}
		}
		if err != nil {
			break
		}
	}
	return out
}

// toolPaths is every file a transcript line's tool calls name.
func toolPaths(line []byte) []string {
	var rec struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &rec) != nil {
		return nil
	}
	var blocks []struct {
		Type  string `json:"type"`
		Input struct {
			FilePath     string `json:"file_path"`
			NotebookPath string `json:"notebook_path"`
		} `json:"input"`
	}
	if json.Unmarshal(rec.Message.Content, &blocks) != nil {
		return nil
	}
	var out []string
	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		if p := strings.TrimSpace(b.Input.FilePath + b.Input.NotebookPath); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// repoDirs is the folders from dir up to the git root above it, or nothing when dir is in no repository.
func repoDirs(dir string) []string {
	var dirs []string
	for d := dir; len(dirs) < 24; d = filepath.Dir(d) {
		dirs = append(dirs, d)
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return dirs
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	return nil
}

// heldBy finds a relative path under a folder the session touched, and answers that folder. It is only ever said,
// never opened.
func heldBy(want string, dirs []string) string {
	if filepath.IsAbs(want) || strings.HasPrefix(want, "~") || strings.Contains(filepath.ToSlash(want), "..") {
		return ""
	}
	for _, d := range dirs {
		if _, err := os.Lstat(filepath.Join(d, want)); err == nil {
			return filepath.ToSlash(d)
		}
	}
	return ""
}
