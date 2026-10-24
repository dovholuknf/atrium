package gitsync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// EVERY ITEM'S CHANGELOG ENTRY, READ ONCE PER REF TIP. A lifecycle pass reads landing for each open item, against claude/main
// and against origin's main, and a `git log -- changelog/*/*-<id>.md` per item and ref kept twenty git children alive on
// the live hub. One `git log` per tip lists every added entry and the files of every commit that touched one, and it is
// kept until the tip moves. Whether it moved is read off the ref store's own files, so a pass where nothing moved spawns
// no git at all.

// landIndex is what one tip's history says of every item's changelog entry.
type landIndex struct {
	done chan struct{}
	err  error
	// added is the first commit that added the item's entry, by item id.
	added map[string]string
	// paths are the files of the non-merge commits that touched the item's entry, newest first, each once.
	paths map[string][]string
}

type landIndexCache struct {
	mu sync.Mutex
	// idx is keyed by dir and tip sha.
	idx map[string]*landIndex
	// tips is the last resolved tip of a ref, keyed by dir and ref, kept while its stamp is unchanged.
	tips map[string]refTip
	// unknown is a commit the mirror lacked, keyed by dir, sha and the stamp of main at the time.
	unknown map[string]bool
}

type refTip struct{ stamp, sha string }

// landIndexMax bounds how many tips' indexes are kept. A pass reads at most claude/main, main and the forge's main.
const landIndexMax = 8

// changelogIndexCap bounds the one log's output: every changelog commit with every file it touched.
const changelogIndexCap = 64 << 20

// refStamp changes whenever ref may have moved: the reftable stack's table list, the loose ref, packed-refs' size and
// time. Empty when none can be read, and then the tip is always asked of git.
func refStamp(dir, ref string) string {
	var b strings.Builder
	if l, err := os.ReadFile(filepath.Join(dir, "reftable", "tables.list")); err == nil {
		b.WriteString("rt:")
		b.Write(l)
	}
	if l, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(ref))); err == nil {
		b.WriteString("|loose:")
		b.Write(l)
	}
	if fi, err := os.Stat(filepath.Join(dir, "packed-refs")); err == nil {
		fmt.Fprintf(&b, "|packed:%d:%d", fi.Size(), fi.ModTime().UnixNano())
	}
	return b.String()
}

// resolveTip is ref's commit, "" when the repository has no such ref. A sha is its own tip. Git is asked only when the
// ref's stamp changed since the last ask.
func (s *Store) resolveTip(ctx context.Context, dir, ref string) (string, error) {
	if ValidSHA(ref) {
		return ref, nil
	}
	c := &s.landIdx
	key := dir + "\x00" + ref
	stamp := refStamp(dir, ref)
	c.mu.Lock()
	t, ok := c.tips[key]
	c.mu.Unlock()
	if ok && stamp != "" && t.stamp == stamp {
		return t.sha, nil
	}
	out, err := s.git(ctx, dir, "rev-parse", "--verify", "-q", ref+"^{commit}")
	sha := strings.TrimSpace(out)
	switch {
	case err != nil && exitCode(err) == 1:
		sha = ""
	case err != nil:
		return "", err
	}
	c.mu.Lock()
	if c.tips == nil {
		c.tips = map[string]refTip{}
	}
	c.tips[key] = refTip{stamp: stamp, sha: sha}
	c.mu.Unlock()
	return sha, nil
}

// knownCommit is commitKnown for the forge's tip, which every item of a pass asks about. A commit once known stays known,
// and one the mirror lacked is asked again only once main has moved, which is how the mirror gets it.
func (s *Store) knownCommit(ctx context.Context, dir, sha string) bool {
	c := &s.landIdx
	key := dir + "\x00" + sha
	miss := key + "\x00" + refStamp(dir, MainRef)
	c.mu.Lock()
	_, indexed := c.idx[key]
	gone := c.unknown[miss]
	c.mu.Unlock()
	if indexed {
		return true
	}
	if gone {
		return false
	}
	known, err := s.commitKnown(ctx, dir, sha)
	if err == nil && !known {
		c.mu.Lock()
		if c.unknown == nil || len(c.unknown) > landIndexMax {
			c.unknown = map[string]bool{}
		}
		c.unknown[miss] = true
		c.mu.Unlock()
	}
	return known
}

// landIndexAt is the index of ref's tip, nil when the repository has no such ref. Readers of one tip share one log.
func (s *Store) landIndexAt(ctx context.Context, dir, ref string) (*landIndex, error) {
	tip, err := s.resolveTip(ctx, dir, ref)
	if err != nil || tip == "" {
		return nil, err
	}
	c := &s.landIdx
	key := dir + "\x00" + tip
	c.mu.Lock()
	x, ok := c.idx[key]
	if !ok {
		if c.idx == nil || len(c.idx) >= landIndexMax {
			kept := map[string]*landIndex{}
			for k, v := range c.idx {
				if !v.isDone() {
					kept[k] = v
				}
			}
			c.idx = kept
		}
		x = &landIndex{done: make(chan struct{})}
		c.idx[key] = x
	}
	c.mu.Unlock()
	if ok {
		select {
		case <-x.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if x.err != nil {
			return nil, x.err
		}
		return x, nil
	}
	x.added, x.paths, x.err = s.readLandIndex(ctx, dir, tip)
	close(x.done)
	if x.err != nil {
		c.mu.Lock()
		if c.idx[key] == x {
			delete(c.idx, key)
		}
		c.mu.Unlock()
		return nil, x.err
	}
	return x, nil
}

func (x *landIndex) isDone() bool {
	select {
	case <-x.done:
		return true
	default:
		return false
	}
}

// readLandIndex is the one log of a tip: every non-merge commit that touched a changelog entry, newest first, with every
// file it touched and how.
func (s *Store) readLandIndex(ctx context.Context, dir, tip string) (map[string]string, map[string][]string, error) {
	out, err := s.h.runner().GitCapped(ctx, dir, s.env(), changelogIndexCap, "log", "--no-merges", "--no-renames",
		"--full-diff", "--name-status", "--format=commit %H", tip, "--", "changelog/")
	if err != nil {
		return nil, nil, err
	}
	added := map[string]string{}
	paths := map[string][]string{}
	seen := map[string]map[string]bool{}
	flush := func(files, ids []string) {
		for _, id := range ids {
			if seen[id] == nil {
				seen[id] = map[string]bool{}
			}
			for _, f := range files {
				if !seen[id][f] {
					seen[id][f] = true
					paths[id] = append(paths[id], f)
				}
			}
		}
	}
	var commit string
	var files, ids []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if c, ok := strings.CutPrefix(l, "commit "); ok {
			flush(files, ids)
			commit, files, ids = c, nil, nil
			continue
		}
		status, path, ok := strings.Cut(l, "\t")
		if !ok || commit == "" {
			continue
		}
		files = append(files, path)
		if id, ok := changelogID(path); ok {
			ids = append(ids, id)
			if status == "A" {
				added[id] = commit
			}
		}
	}
	flush(files, ids)
	return added, paths, nil
}

// changelogID is the item id of changelog/<dept>/<yyyy-mm-dd>-<id>.md.
func changelogID(path string) (string, bool) {
	parts := strings.Split(path, "/")
	if len(parts) != 3 || parts[0] != "changelog" {
		return "", false
	}
	name, ok := strings.CutSuffix(parts[2], ".md")
	if !ok || len(name) < 12 || name[10] != '-' {
		return "", false
	}
	for i, c := range name[:10] {
		if i == 4 || i == 7 {
			if c != '-' {
				return "", false
			}
		} else if c < '0' || c > '9' {
			return "", false
		}
	}
	return name[11:], true
}
