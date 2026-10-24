// Package wtfree is what a close and a cull share to free a worktree's disk: measuring it, and removing what git
// leaves behind.
package wtfree

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/nowindow"
)

// gitWait bounds the git command that lists worktrees.
const gitWait = time.Minute

// WHAT GIT LEAVES. `git worktree remove` drops the worktree from its list and deletes its tracked files, and on
// Windows an ignored tree such as node_modules, with paths past 260 characters, can stay on disk. A worktree is not
// freed until its path is gone, so the caller removes the rest itself and says what is left when it cannot.

// DirSize is the bytes of every regular file under dir, 0 for a directory that is not there. Symlinks are not
// followed, so a link out of the tree is not counted. Ignored files count.
func DirSize(ctx context.Context, dir string) int64 {
	var n int64
	_ = filepath.WalkDir(LongPath(dir), func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return fs.SkipAll
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	if _, err := os.Stat(LongPath(dir)); err != nil {
		return 0
	}
	return n
}

// RemoveAllSoon deletes a folder, trying a few times: a process just stopped on Windows lets go of its files a moment
// after it is gone.
func RemoveAllSoon(dir string) error {
	var err error
	for i := 0; i < 10; i++ {
		if err = os.RemoveAll(dir); err == nil {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return err
}

// EqPath compares two paths the way the platform does.
func EqPath(a, b string) bool {
	if filepath.Separator == '\\' {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// Listed is whether git still names path as a worktree of repo. A failure to ask reads as listed.
func Listed(repo, path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), gitWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "worktree", "list", "--porcelain")
	cmd.Dir = filepath.FromSlash(repo)
	nowindow.Hide(cmd)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		return true
	}
	want := filepath.Clean(filepath.FromSlash(path))
	wantReal := realPath(want)
	for _, l := range strings.Split(string(out), "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(l), "worktree "); ok {
			got := filepath.Clean(filepath.FromSlash(p))
			if EqPath(got, want) || EqPath(realPath(got), wantReal) {
				return true
			}
		}
	}
	return false
}

// realPath is path with symlinks resolved, as git reports it (macOS lists /private/var for /var), or path itself when it
// cannot be resolved.
func realPath(path string) string {
	if r, err := filepath.EvalSymlinks(LongPath(path)); err == nil {
		return filepath.Clean(strings.TrimPrefix(r, `\\?\`))
	}
	return path
}

// RemoveLeftover deletes what is still at path once git no longer lists it as a worktree, then checks it is gone. An
// error says what is left. It never runs on a path git still lists, and never on a shallow path or one holding repo.
func RemoveLeftover(repo, path string) error {
	dir := filepath.Clean(filepath.FromSlash(path))
	if _, err := os.Lstat(LongPath(dir)); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	parts := strings.FieldsFunc(dir, func(r rune) bool { return r == '\\' || r == '/' })
	if !filepath.IsAbs(dir) || len(parts) < 3 {
		return fmt.Errorf("%s was left on disk and is too close to the root to delete", path)
	}
	r := filepath.Clean(filepath.FromSlash(repo))
	if EqPath(r, dir) || strings.HasPrefix(strings.ToLower(r), strings.ToLower(dir)+string(filepath.Separator)) {
		return fmt.Errorf("%s was left on disk and holds its repository, so it is not deleted", path)
	}
	if Listed(repo, path) {
		return fmt.Errorf("%s is still a worktree of its repository", path)
	}
	err := RemoveAllSoon(LongPath(dir))
	if _, serr := os.Lstat(LongPath(dir)); errors.Is(serr, fs.ErrNotExist) {
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
	if ents, err := os.ReadDir(LongPath(dir)); err == nil {
		for i, e := range ents {
			if i == 5 {
				top = append(top, "...")
				break
			}
			top = append(top, e.Name())
		}
	}
	_ = filepath.WalkDir(LongPath(dir), func(string, fs.DirEntry, error) error {
		n++
		return nil
	})
	return top, n - 1
}
