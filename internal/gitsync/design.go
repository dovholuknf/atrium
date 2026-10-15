package gitsync

import (
	"context"
	"strconv"
	"strings"
)

// WHAT THE ITEM LIFECYCLE'S DESIGN STEPS READ OF THE HUB'S STORE (L2). Four reads, each a git command in a bare repository
// the hub holds, with no shell, a bound, and nothing taken from a caller that was not checked first: a sha is hex and a path
// has no `..` and no leading dash. Nothing here writes. See docs/rnd/item-lifecycle-design.md, section 3.

// FindCommit names the repository in the store that holds the commit, and its full id. A sha that is not hex, or that no
// repository holds, is not found. The store holds a handful of repositories, so this is one cheap call each.
func (s *Store) FindCommit(ctx context.Context, sha string) (name, full string, ok bool) {
	sha = strings.ToLower(strings.TrimSpace(sha))
	if !isHex(sha) || len(sha) < 7 || len(sha) > 64 {
		return "", "", false
	}
	entries, err := s.List()
	if err != nil {
		return "", "", false
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	for _, e := range entries {
		out, err := s.git(ctx, e.Dir, "rev-parse", "--verify", "-q", sha+"^{commit}")
		if err != nil {
			continue
		}
		if f := strings.TrimSpace(out); ValidSHA(f) {
			return e.Ref.Name(), f, true
		}
	}
	return "", "", false
}

// CommitFiles is the paths a commit changes, as git names them. A repository or a commit the store lacks is no files.
func (s *Store) CommitFiles(ctx context.Context, name, sha string) ([]string, error) {
	dir, err := s.bareFor(name)
	if err != nil || dir == "" || !ValidSHA(sha) {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	out, err := s.git(ctx, dir, "diff-tree", "--no-commit-id", "--name-only", "-r", "--root", "-z", sha)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(out, "\x00") {
		if f = strings.TrimSpace(f); f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

// safeRepoPath is whether a path is one git may be handed after `--`: relative, with no `..` and nothing that git or a
// disk reads as more than a name.
func safeRepoPath(p string) bool {
	if p == "" || len(p) > 400 || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "-") || strings.ContainsAny(p, "\x00\r\n\\") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || part == "" {
			return false
		}
	}
	return true
}

// LastTouch is the newest commit that changed path, among the commits built on base in any branch the store holds. It is
// base itself when nothing built on it changed the path. THIS IS HOW A DESIGN DOC "HAS MOVED ON": the approval is pinned to
// one commit, and a later one that changes the file is what makes it stale.
func (s *Store) LastTouch(ctx context.Context, name, base, path string) (string, error) {
	dir, err := s.bareFor(name)
	if err != nil || dir == "" || !ValidSHA(base) || !safeRepoPath(path) {
		return base, err
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	out, err := s.git(ctx, dir, "log", "-n1", "--format=%H", "--all", "--ancestry-path", "^"+base, "--", path)
	if err != nil {
		return base, err
	}
	if sha := strings.TrimSpace(out); ValidSHA(sha) {
		return sha, nil
	}
	return base, nil
}

// TouchesSince is how many commits built on base changed path. Zero is a design that has not moved.
func (s *Store) TouchesSince(ctx context.Context, name, base, path string) (int, error) {
	dir, err := s.bareFor(name)
	if err != nil || dir == "" || !ValidSHA(base) || !safeRepoPath(path) {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	out, err := s.git(ctx, dir, "rev-list", "--count", "--all", "--ancestry-path", "^"+base, "--", path)
	if err != nil {
		return 0, err
	}
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n, nil
}
