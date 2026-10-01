package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// A TURN is what lies between one real prompt and the next, in the card's transcript. The
// edit tool calls inside it are what the per-reply "N files changed" chip counts
// (docs/rnd/changes-view-design.md, `?turn=`).
//
// EVERY PATH HERE IS UNTRUSTED. A transcript is text an agent wrote, so a path in it is a
// claim and never an address: it is resolved through safepath against the card's worktree
// before anything reads it, and one that lands outside is counted and never named.
// Nothing in this file runs git. `/replies` reads it on every turn end, and a git process
// per reply would be the cost of opening a card page.

// editTools are the tool calls whose input names a file they changed.
var editTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

// editCall is one edit tool call and the path it named, as the transcript wrote it.
type editCall struct {
	at   time.Time
	path string
}

// turnIndex is everything a transcript says about edits, in file order.
type turnIndex struct {
	prompts []time.Time // real prompts, ascending
	texts   []time.Time // assistant records with text, ascending
	edits   []editCall
}

// span is the turn holding `at`: from the latest prompt at or before it to the next
// prompt after it. A zero end means the turn is still open.
func (x *turnIndex) span(at time.Time) (start, end time.Time) {
	i := sort.Search(len(x.prompts), func(i int) bool { return x.prompts[i].After(at) })
	if i > 0 {
		start = x.prompts[i-1]
	}
	if i < len(x.prompts) {
		end = x.prompts[i]
	}
	return start, end
}

// hasReply says a reply's text was written at exactly `at`.
func (x *turnIndex) hasReply(at time.Time) bool {
	i := sort.Search(len(x.texts), func(i int) bool { return !x.texts[i].Before(at) })
	return i < len(x.texts) && x.texts[i].Equal(at)
}

// editsIn is the edit calls made in [start, end), end zero meaning no end.
func (x *turnIndex) editsIn(start, end time.Time) []editCall {
	var out []editCall
	for _, e := range x.edits {
		if e.at.Before(start) || (!end.IsZero() && !e.at.Before(end)) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// scanTurns reads a whole transcript once. Sidechain (subagent) lines are skipped: a
// subagent's edits are not this turn's reply.
func scanTurns(path string) (*turnIndex, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	x := &turnIndex{}
	for sc.Scan() {
		line := sc.Bytes()
		user := bytes.Contains(line, []byte(`"user"`))
		if !user && !bytes.Contains(line, []byte(`"assistant"`)) {
			continue
		}
		if user && bytes.Contains(line, []byte(`"tool_result"`)) {
			continue
		}
		var rec struct {
			Type      string `json:"type"`
			Sidechain bool   `json:"isSidechain"`
			Meta      bool   `json:"isMeta"`
			Timestamp string `json:"timestamp"`
			Origin    *struct {
				Kind string `json:"kind"`
			} `json:"origin"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &rec) != nil || rec.Sidechain {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
		if err != nil {
			continue
		}
		at = at.UTC()
		switch rec.Type {
		case "user":
			if rec.Meta || (rec.Origin != nil && rec.Origin.Kind != "" && rec.Origin.Kind != "human") {
				continue
			}
			var content any
			if json.Unmarshal(rec.Message.Content, &content) != nil {
				continue
			}
			if _, ok := promptOf(content); ok {
				x.prompts = append(x.prompts, at)
			}
		case "assistant":
			var blocks []struct {
				Type  string `json:"type"`
				Text  string `json:"text"`
				Name  string `json:"name"`
				Input struct {
					FilePath     string `json:"file_path"`
					NotebookPath string `json:"notebook_path"`
				} `json:"input"`
			}
			if json.Unmarshal(rec.Message.Content, &blocks) != nil {
				continue
			}
			text := false
			for _, b := range blocks {
				switch {
				case b.Type == "text" && strings.TrimSpace(b.Text) != "":
					text = true
				case b.Type == "tool_use" && editTools[b.Name]:
					p := b.Input.FilePath
					if p == "" {
						p = b.Input.NotebookPath
					}
					if p != "" {
						x.edits = append(x.edits, editCall{at: at, path: p})
					}
				}
			}
			if text {
				x.texts = append(x.texts, at)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return x, nil
}

// turnCache holds the last scan per transcript on its size and modification time, since
// the page asks at every turn end and a transcript only grows. Its own lock.
var turnCache = struct {
	mu sync.Mutex
	m  map[string]turnCached
}{m: map[string]turnCached{}}

type turnCached struct {
	size  int64
	mtime time.Time
	x     *turnIndex
}

func turnsOf(path string) (*turnIndex, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	turnCache.mu.Lock()
	c, ok := turnCache.m[path]
	turnCache.mu.Unlock()
	if ok && c.size == info.Size() && c.mtime.Equal(info.ModTime()) {
		return c.x, nil
	}
	x, err := scanTurns(path)
	if err != nil {
		return nil, err
	}
	turnCache.mu.Lock()
	if len(turnCache.m) > 50 {
		turnCache.m = map[string]turnCached{}
	}
	turnCache.m[path] = turnCached{size: info.Size(), mtime: info.ModTime(), x: x}
	turnCache.mu.Unlock()
	return x, nil
}

// insideWorktree resolves each named path through safepath against the worktree and
// returns the distinct ones inside it, relative to it with forward slashes, and how many
// distinct ones landed outside (or could not be resolved). A relative path is dropped: a
// transcript's tool calls name absolute ones, and a relative one means whatever the
// reader's directory is.
func insideWorktree(worktree string, calls []editCall) (rels []string, outside int) {
	worktree = strings.TrimSpace(worktree)
	if worktree == "" {
		return nil, len(distinctPaths(calls))
	}
	realRoot, err := filepath.EvalSymlinks(filepath.FromSlash(worktree))
	if err != nil {
		return nil, len(distinctPaths(calls))
	}
	seen := map[string]bool{}
	for _, p := range distinctPaths(calls) {
		if !filepath.IsAbs(filepath.FromSlash(p)) {
			outside++
			continue
		}
		real, err := safepath.Contained(worktree, p)
		if err != nil {
			outside++
			continue
		}
		rel, ok := under(realRoot, real)
		if !ok || rel == "" {
			outside++
			continue
		}
		rel = filepath.ToSlash(rel)
		if !seen[rel] {
			seen[rel] = true
			rels = append(rels, rel)
		}
	}
	sort.Strings(rels)
	return rels, outside
}

func distinctPaths(calls []editCall) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range calls {
		if !seen[c.path] {
			seen[c.path] = true
			out = append(out, c.path)
		}
	}
	return out
}

// under is target relative to root, when target is inside it. Both are resolved already.
func under(root, target string) (string, bool) {
	root = strings.TrimRight(root, string(filepath.Separator))
	if len(target) <= len(root) {
		return "", false
	}
	head := target[:len(root)]
	if head != root && !(runtime.GOOS == "windows" && strings.EqualFold(head, root)) {
		return "", false
	}
	if target[len(root)] != filepath.Separator {
		return "", false
	}
	return target[len(root)+1:], true
}

// cardTranscript is the transcript a card's replies are read from, or "" when atrium
// cannot read one (a runner that is not Claude, or no file). The same choice /replies makes.
func (d *Daemon) cardTranscript(t *store.Task) string {
	if d.usage == nil || !d.usage.isClaude(t.Runner) {
		return ""
	}
	session := strings.TrimSpace(t.ResumeID)
	if d.ctx != nil {
		session = d.ctx.sessionOf(t)
	}
	if session == "" {
		return ""
	}
	return d.usage.transcript(t.Worktree, session)
}

// fillEdited sets each reply's `edited`: how many files the edit calls of its turn named
// inside the worktree. From the transcript alone, no git. A failure leaves the counts 0.
func fillEdited(t *store.Task, transcript string, replies []Reply) {
	if len(replies) == 0 {
		return
	}
	x, err := turnsOf(transcript)
	if err != nil {
		return
	}
	byTurn := map[time.Time]int{}
	for i := range replies {
		start, end := x.span(replies[i].At)
		n, ok := byTurn[start]
		if !ok {
			rels, _ := insideWorktree(t.Worktree, x.editsIn(start, end))
			n = len(rels)
			byTurn[start] = n
		}
		replies[i].Edited = n
	}
}
