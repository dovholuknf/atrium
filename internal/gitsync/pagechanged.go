package gitsync

import (
	"context"
	"strings"
	"sync"
)

// PAGE CHANGED SINCE. docs/rnd/doc-debt-from-features.md sections 2.7 and 4 (phase 4). For a stale page a doc item names,
// whether the page changed on claude/main of the hub's mirror after the feature's `landed` sha, and the newest commit that
// changed it. Read on demand and remembered in memory by landed sha, path and the head it was read at, so a new head asks
// again and nothing is stored.

// PageChange is the newest commit that touched a page since a sha.
type PageChange struct {
	SHA  string
	Date string
}

type pageKey struct{ name, since, path, head string }

// pageChanges is the answers already read. A zero value is ready.
type pageChanges struct {
	mu sync.Mutex
	m  map[pageKey]*PageChange
}

// pageChangeMax bounds the remembered answers, which the hub resets rather than evicts: a doc debt is a handful of pages.
const pageChangeMax = 2000

// PageChangedSince is the newest commit on the mirror's claude/main after `since` that changed `path`, and false when none
// did. An error is a mirror that is not held, a head or a sha the mirror does not have, or a git that failed, and the caller
// leaves the hint out.
func (s *Store) PageChangedSince(ctx context.Context, name, since, path string) (PageChange, bool, error) {
	var none PageChange
	dir, err := s.bareFor(name)
	if err != nil {
		return none, false, err
	}
	if dir == "" {
		return none, false, refuse("the hub holds no mirror of %s", name)
	}
	if !ValidSHA(since) || !safePagePath(path) {
		return none, false, refuse("that is not a sha and a path inside the repository")
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	out, err := s.git(ctx, dir, "rev-parse", "--verify", "-q", ClaudeMainRef+"^{commit}")
	if err != nil {
		return none, false, err
	}
	head := strings.TrimSpace(out)
	k := pageKey{name, since, path, head}
	s.pages.mu.Lock()
	c, hit := s.pages.m[k]
	s.pages.mu.Unlock()
	if hit {
		if c == nil {
			return none, false, nil
		}
		return *c, true, nil
	}
	out, err = s.h.runner().GitEnv(ctx, dir, append(s.env(), "GIT_LITERAL_PATHSPECS=1"),
		"log", "-1", "--format=%H %cI", since+".."+head, "--", path)
	if err != nil {
		return none, false, err
	}
	var got *PageChange
	if f := strings.Fields(out); len(f) == 2 {
		got = &PageChange{SHA: f[0], Date: f[1]}
	}
	s.pages.mu.Lock()
	if s.pages.m == nil || len(s.pages.m) >= pageChangeMax {
		s.pages.m = map[pageKey]*PageChange{}
	}
	s.pages.m[k] = got
	s.pages.mu.Unlock()
	if got == nil {
		return none, false, nil
	}
	return *got, true, nil
}

// safePagePath is a path that stays inside the repository, with no newline or trace of a way out.
func safePagePath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\n\r\x00\\") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}
