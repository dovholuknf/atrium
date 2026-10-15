package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// GET /v1/tasks/{id}/replies?all=1: every reply the card's transcripts still hold, for the copy clean button, which
// has to match a selection against anything still on the terminal's scrollback and not only the last few replies.
//
// The current conversation first, then the other conversations in the same project folder (what a /clear or an
// earlier resume id left behind), newest file first. Oldest reply first in the answer, as the page's own is. No
// prompts and no paging: the one reader is a text search.
//
// BOUNDED. Each file is read from its tail, and every file together reads at most `repliesAllBudget` bytes and
// `repliesAllFiles` files, so a huge history costs a fixed amount and the oldest part of it is what is left out.
// Replies are cut at `replyTextMax` like every other read. A card with no readable claude transcript answers like the
// ordinary page, from the screen, which the board ignores.
const (
	repliesAllBudget int64 = 8 << 20
	repliesAllFiles        = 12
)

// repliesAllCache keeps the last answer per card on the sizes and times of the files it was read from, so a
// selection made twice costs one read. Its own lock.
var repliesAllCache = struct {
	mu sync.Mutex
	m  map[string]repliesAllCached
}{m: map[string]repliesAllCached{}}

type repliesAllCached struct {
	key     string
	replies []Reply
}

func (d *Daemon) repliesAll(taskID string) (*RepliesView, error) {
	t, err := d.st.Get(taskID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errNoSuchCard
	}
	cur, ok := claudeReader{d}.path(t)
	if !ok {
		return d.repliesPage(taskID, repliesDefault, time.Time{})
	}
	files := append([]string{cur}, siblingTranscripts(cur)...)
	if len(files) > repliesAllFiles {
		files = files[:repliesAllFiles]
	}
	key := ""
	for _, p := range files {
		if info, err := os.Stat(p); err == nil {
			key += fmt.Sprintf("%s|%d|%d;", p, info.Size(), info.ModTime().UnixNano())
		}
	}
	repliesAllCache.mu.Lock()
	c, hit := repliesAllCache.m[taskID]
	repliesAllCache.mu.Unlock()
	if hit && c.key == key {
		return &RepliesView{Source: "transcript", Replies: append([]Reply{}, c.replies...)}, nil
	}
	var out []Reply
	left := d.hk.repliesAllBudget
	for _, p := range files {
		if left <= 0 {
			break
		}
		rs, read, err := readReplyTail(p, left)
		if err != nil {
			if p == cur {
				logRepliesFallback(p, err)
				return d.repliesPage(taskID, repliesDefault, time.Time{})
			}
			continue
		}
		left -= read
		out = append(append([]Reply{}, rs...), out...)
	}
	repliesAllCache.mu.Lock()
	if len(repliesAllCache.m) > 100 {
		repliesAllCache.m = map[string]repliesAllCached{}
	}
	repliesAllCache.m[taskID] = repliesAllCached{key: key, replies: out}
	repliesAllCache.mu.Unlock()
	return &RepliesView{Source: "transcript", Replies: append([]Reply{}, out...)}, nil
}

// readReplyTail is the replies in the last max bytes of a file, oldest first, and how many bytes that was. A read that
// starts mid-file begins mid-line, and the scan drops a line it cannot parse.
func readReplyTail(path string, max int64) ([]Reply, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	read := info.Size()
	if read > max {
		read = max
		if _, err := f.Seek(info.Size()-read, 0); err != nil {
			return nil, 0, err
		}
	}
	rs, _, _, err := scanTranscriptWindow(f)
	return rs, read, err
}

// siblingTranscripts are the other conversations' files beside cur, newest first.
func siblingTranscripts(cur string) []string {
	ents, err := os.ReadDir(filepath.Dir(cur))
	if err != nil {
		return nil
	}
	type f struct {
		path string
		at   time.Time
	}
	var fs []f
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		p := filepath.Join(filepath.Dir(cur), e.Name())
		if p == cur {
			continue
		}
		if info, err := e.Info(); err == nil {
			fs = append(fs, f{p, info.ModTime()})
		}
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i].at.After(fs[j].at) })
	out := make([]string, len(fs))
	for i, x := range fs {
		out[i] = x.path
	}
	return out
}
