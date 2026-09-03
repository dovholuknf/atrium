package store

import (
	"os"
	"path/filepath"
	"strings"
)

// Which repository a directory belongs to, and which branch is checked out.
//
// Atrium named a card after the last segment of its working directory, which
// is right often enough to look correct and wrong in the two cases that matter
// most. A session started inside a subdirectory took the subdirectory's name:
// running claude in `dotfiles/powershell` produced a card called `powershell`,
// which is not a project anybody has. And a worktree is named for its branch,
// so two worktrees of the same repository became `2026-08-21` and
// `aug-21-2026` with nothing on either saying they were both `ziti-tv`.
//
// The gwt session ledger has always called these `main:atrium` and
// `tangent:help-prune-worktrees`, so the board and the ledger disagreed about
// the name of the same session.
//
// Read rather than executed. `git rev-parse` would answer this and would mean
// a subprocess on a path that registration sits on, and registration is called
// by every hook of every session. The two files involved are small, the
// formats are stable, and a failure to parse them costs a fallback to the
// directory name, which is where this started.

// GitInfo is what a directory says about itself.
type GitInfo struct {
	// Repo is the repository's own directory name, not the working
	// directory's. A subdirectory and a worktree both resolve to it.
	Repo string
	// Branch is the checked-out branch. Empty on a detached head, which is a
	// real state and not an error: the card falls back to naming the repo.
	Branch string
}

// gitWalkLimit bounds how far up the tree to look for a repository.
//
// A directory that is not in one otherwise walks to the filesystem root on
// every registration. Deep enough for any real checkout and short enough that
// the answer costs nothing when it is no.
const gitWalkLimit = 24

// ReadGitInfo answers what repository and branch a directory belongs to.
//
// A zero value means "not in a repository, or could not tell", and every
// caller treats that the same way: keep whatever it had.
func ReadGitInfo(dir string) GitInfo {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return GitInfo{}
	}
	dir = filepath.FromSlash(dir)

	for i := 0; i < gitWalkLimit; i++ {
		dotgit := filepath.Join(dir, ".git")
		fi, err := os.Lstat(dotgit)
		if err == nil {
			if fi.IsDir() {
				// An ordinary checkout. The repository is this directory.
				return GitInfo{
					Repo:   filepath.Base(dir),
					Branch: headBranch(dotgit),
				}
			}
			// A linked worktree. `.git` is a file naming the real git
			// directory, which lives inside the main repository, so the
			// repository's name is recoverable from that path and the branch
			// is in the HEAD beside it.
			return worktreeInfo(dotgit)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return GitInfo{}
}

// worktreeInfo reads a `.git` FILE, which is how a linked worktree points at
// its real git directory: `gitdir: /path/to/repo/.git/worktrees/<name>`.
//
// The repository's name is the directory holding that `.git`, which is why the
// path is walked back up rather than the name being taken from the worktree
// directory. The worktree directory is named for the branch, and using it is
// the bug this exists to fix.
func worktreeInfo(dotgitFile string) GitInfo {
	raw, err := os.ReadFile(dotgitFile)
	if err != nil {
		return GitInfo{}
	}
	line := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(line, "gitdir:") {
		return GitInfo{}
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if gitdir == "" {
		return GitInfo{}
	}
	gitdir = filepath.FromSlash(gitdir)
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(filepath.Dir(dotgitFile), gitdir)
	}

	out := GitInfo{Branch: headBranch(gitdir)}

	// .../<repo>/.git/worktrees/<name> -> <repo>. Walked by name rather than
	// by counting segments, since a git directory can be somewhere else
	// entirely and then there is no repository name to find here.
	dir := gitdir
	for i := 0; i < gitWalkLimit; i++ {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		if filepath.Base(dir) == ".git" {
			out.Repo = filepath.Base(parent)
			return out
		}
		dir = parent
	}
	return out
}

// headBranch reads the branch out of a git directory's HEAD.
//
// `ref: refs/heads/<branch>` for a branch, a bare commit id when detached. A
// detached head answers empty, which is correct: there is no branch to name.
func headBranch(gitdir string) string {
	raw, err := os.ReadFile(filepath.Join(gitdir, "HEAD"))
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(raw))
	const ref = "ref:"
	if !strings.HasPrefix(line, ref) {
		return ""
	}
	name := strings.TrimSpace(strings.TrimPrefix(line, ref))
	// Only the last segment. `refs/heads/feature/thing` is the branch
	// `feature/thing`, so the prefix is trimmed rather than the path split.
	name = strings.TrimPrefix(name, "refs/heads/")
	return name
}

// TitleFor names a card the way the gwt session ledger names a session.
//
// `branch:repo`, so the board and the ledger call the same session the same
// thing. The alternative was a name that is right most of the time and wrong
// on exactly the two directories somebody works in most: a subdirectory of a
// repository, and a worktree.
//
// Falls back through what is actually known. A directory outside any
// repository has only its own name, which is where atrium started and is
// still the right answer when there is nothing better.
func TitleFor(repo, branch, worktree, fallback string) string {
	switch {
	case repo != "" && branch != "":
		return branch + ":" + repo
	case repo != "":
		return repo
	case fallback != "":
		return fallback
	case worktree != "":
		return filepath.Base(filepath.FromSlash(worktree))
	}
	return "untitled"
}
