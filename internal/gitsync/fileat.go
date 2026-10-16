package gitsync

import (
	"context"
	"strings"
)

// A FILE AS A COMMIT HELD IT. docs/rnd/doc-debt-from-features.md section 2.8 (the check at `landed`): the hub reads a
// feature's docs/changes/<item>.md from the commit it landed as. Nothing is checked out and nothing is written.

// fileAtMax bounds what is kept of a file, which is a test plan and never needs more than its top.
const fileAtMax = 64 << 10

// FileAt is the text of `path` in the commit `sha` of the repository `name`, and false when the commit holds no such file.
// An error is a mirror that is not held, a sha or a path that is not safe to hand to git, or a git that failed.
func (s *Store) FileAt(ctx context.Context, name, sha, path string) (string, bool, error) {
	dir, err := s.bareFor(name)
	if err != nil {
		return "", false, err
	}
	if dir == "" {
		return "", false, refuse("the hub holds no mirror of %s", name)
	}
	if !ValidSHA(sha) || !safePagePath(path) || strings.HasPrefix(path, "-") {
		return "", false, refuse("that is not a sha and a path inside the repository")
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	// ls-tree says a missing path with an empty answer, where cat-file says it with an error that looks like any other.
	out, err := s.git(ctx, dir, "ls-tree", "--name-only", sha, "--", path)
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(out) == "" {
		return "", false, nil
	}
	out, err = s.git(ctx, dir, "cat-file", "-p", sha+":"+path)
	if err != nil {
		return "", false, err
	}
	if len(out) > fileAtMax {
		out = out[:fileAtMax]
	}
	return out, true, nil
}
