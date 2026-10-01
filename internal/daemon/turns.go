package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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

// turnScanned counts the transcript bytes scanTurns has read, for a test that checks a growing file
// is read from where it left off and not from the start.
var turnScanned atomic.Int64

// fingerprintLen is how many bytes before the cached offset are compared to tell the file it was
// read from from one that replaced it.
const fingerprintLen = 256

// clone is a copy the caller may append to without touching the one a reader holds.
func (x *turnIndex) clone() *turnIndex {
	if x == nil {
		return &turnIndex{}
	}
	return &turnIndex{
		prompts: append([]time.Time(nil), x.prompts...),
		texts:   append([]time.Time(nil), x.texts...),
		edits:   append([]editCall(nil), x.edits...),
	}
}

// scanTurnsFrom reads transcript lines from `offset` into a copy of base, and returns it with the
// offset of the first byte not read. A line with no newline yet is left for the next read, since
// the file is being written. Sidechain (subagent) lines are skipped: a subagent's edits are not this
// turn's reply.
func scanTurnsFrom(path string, base *turnIndex, offset int64) (*turnIndex, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, 0, err
	}
	x := base.clone()
	br := bufio.NewReaderSize(f, 256<<10)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				// Whatever is after the last newline is a line still being written.
				turnScanned.Add(int64(len(line)))
				return x, offset, nil
			}
			return nil, 0, err
		}
		offset += int64(len(line))
		turnScanned.Add(int64(len(line)))
		x.feed(line)
	}
}

func scanTurns(path string) (*turnIndex, error) {
	x, _, err := scanTurnsFrom(path, nil, 0)
	return x, err
}

// feed takes one transcript line.
func (x *turnIndex) feed(line []byte) {
	if len(line) > 8<<20 {
		return
	}
	user := bytes.Contains(line, []byte(`"user"`))
	if !user && !bytes.Contains(line, []byte(`"assistant"`)) {
		return
	}
	if user && bytes.Contains(line, []byte(`"tool_result"`)) {
		return
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
		return
	}
	at, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
	if err != nil {
		return
	}
	at = at.UTC()
	switch rec.Type {
	case "user":
		if rec.Meta || (rec.Origin != nil && rec.Origin.Kind != "" && rec.Origin.Kind != "human") {
			return
		}
		var content any
		if json.Unmarshal(rec.Message.Content, &content) != nil {
			return
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
			return
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

// turnCache holds the last scan per transcript, and how far into the file it got. A transcript
// only grows, so the next read starts at that offset. A file that got shorter, or whose bytes before
// the offset are not the ones read, was replaced and is read again from the start. Its own lock.
var turnCache = struct {
	mu sync.Mutex
	m  map[string]turnCached
}{m: map[string]turnCached{}}

type turnCached struct {
	size   int64
	mtime  time.Time
	offset int64  // the first byte not yet read
	print  []byte // the fingerprintLen bytes before offset
	x      *turnIndex
}

// fingerprint is the bytes just before offset.
func fingerprint(path string, offset int64) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	n := int64(fingerprintLen)
	if offset < n {
		n = offset
	}
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, offset-n); err != nil {
		return nil
	}
	return buf
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
	var base *turnIndex
	var from int64
	if ok && info.Size() >= c.offset && bytes.Equal(fingerprint(path, c.offset), c.print) {
		base, from = c.x, c.offset
	}
	x, offset, err := scanTurnsFrom(path, base, from)
	if err != nil {
		return nil, err
	}
	turnCache.mu.Lock()
	if len(turnCache.m) > 50 {
		turnCache.m = map[string]turnCached{}
	}
	turnCache.m[path] = turnCached{size: info.Size(), mtime: info.ModTime(), offset: offset,
		print: fingerprint(path, offset), x: x}
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
