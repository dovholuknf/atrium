package gitsync

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RoomOwned is what a room owns inside its clone: the refs a sync writes. Everything else in
// the clone is the operator's, and that includes packed-refs.
var RoomOwned = []string{"refs/remotes/hub", "refs/heads/claude/main", "refs/heads/hub-main"}

// CleanLocks removes stale `*.lock` files in gitDir, and ONLY under refs the caller owns.
//
// A git killed mid-fetch leaves ref locks behind and every later git on that repository
// fails with "File exists". A lock is stale when it is older than `older` (CommandBound when
// zero), past which no command of ours is still running.
//
// `owned` is ref prefixes relative to gitDir, like `refs/remotes/hub`. A lock on a ref
// equal to a prefix or under one is removed. `packed-refs` may be listed too. A stale lock
// anywhere else is NOT touched and comes back in `left` in git's own words, so the failure
// that follows is explained. A lock younger than the bound is neither removed nor reported:
// somebody may be using it.
func CleanLocks(gitDir string, owned []string, older time.Duration) (removed, left []string) {
	if older <= 0 {
		older = CommandBound
	}
	cutoff := time.Now().Add(-older)
	check := func(path string) {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.ModTime().After(cutoff) {
			return
		}
		rel, err := filepath.Rel(gitDir, path)
		if err != nil {
			return
		}
		rel = strings.TrimSuffix(filepath.ToSlash(rel), ".lock")
		rel = strings.TrimPrefix(rel, "logs/")
		for _, o := range owned {
			if rel == o || strings.HasPrefix(rel, o+"/") {
				if os.Remove(path) == nil {
					removed = append(removed, path)
				}
				return
			}
		}
		left = append(left, "Unable to create '"+filepath.ToSlash(path)+"': File exists. It is older than "+
			older.String()+" and not a ref atrium owns, so it was left alone")
	}
	// Top level (packed-refs.lock, HEAD.lock, config.lock, index.lock), then refs and reflogs.
	if ents, err := os.ReadDir(gitDir); err == nil {
		for _, e := range ents {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".lock") {
				check(filepath.Join(gitDir, e.Name()))
			}
		}
	}
	for _, sub := range []string{"refs", "logs"} {
		_ = filepath.WalkDir(filepath.Join(gitDir, sub), func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".lock") {
				check(p)
			}
			return nil
		})
	}
	return removed, left
}
