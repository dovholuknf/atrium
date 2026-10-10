package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// WHAT GIT LEAVES. `git worktree remove` drops the worktree from its list and deletes its tracked files, and on
// Windows an ignored tree such as node_modules, with paths past 260 characters, can stay on disk. A close does not call
// that freed: it removes the rest itself, and a path that is still there is said on the row with what is left.

// worktreeListed is whether git still names path as a worktree of repo.
func worktreeListed(repo, path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), closeGitWait)
	defer cancel()
	out, err := gitIn(ctx, repo, "worktree", "list", "--porcelain")
	if err != nil {
		return true
	}
	want := filepath.Clean(filepath.FromSlash(path))
	for _, l := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(l), "worktree "); ok &&
			eqPath(filepath.Clean(filepath.FromSlash(p)), want) {
			return true
		}
	}
	return false
}

// removeLeftover deletes what is still at path once git no longer lists it as a worktree, then checks it is gone. An
// error says what is left. It never runs on a path git still lists, and never on a shallow path or one holding repo.
func removeLeftover(repo, path string) error {
	dir := filepath.Clean(filepath.FromSlash(path))
	if _, err := os.Lstat(longPath(dir)); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	parts := strings.FieldsFunc(dir, func(r rune) bool { return r == '\\' || r == '/' })
	if !filepath.IsAbs(dir) || len(parts) < 3 {
		return fmt.Errorf("%s was left on disk and is too close to the root to delete", path)
	}
	r := filepath.Clean(filepath.FromSlash(repo))
	if eqPath(r, dir) || strings.HasPrefix(strings.ToLower(r), strings.ToLower(dir)+string(filepath.Separator)) {
		return fmt.Errorf("%s was left on disk and holds its repository, so it is not deleted", path)
	}
	if worktreeListed(repo, path) {
		return fmt.Errorf("%s is still a worktree of its repository", path)
	}
	err := removeAllSoon(longPath(dir))
	if _, serr := os.Lstat(longPath(dir)); errors.Is(serr, fs.ErrNotExist) {
		return nil
	}
	left, n := whatIsLeft(dir)
	msg := fmt.Sprintf("%s is still on disk, %d entries left", path, n)
	if len(left) > 0 {
		msg += ": " + strings.Join(left, ", ")
	}
	if err != nil {
		msg += ". " + err.Error()
	}
	return errors.New(msg)
}

// whatIsLeft counts the entries under dir and names the first few at its top.
func whatIsLeft(dir string) (top []string, n int) {
	if ents, err := os.ReadDir(longPath(dir)); err == nil {
		for i, e := range ents {
			if i == 5 {
				top = append(top, "...")
				break
			}
			top = append(top, e.Name())
		}
	}
	_ = filepath.WalkDir(longPath(dir), func(string, fs.DirEntry, error) error {
		n++
		return nil
	})
	return top, n - 1
}
