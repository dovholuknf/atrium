package store

import (
	"os"
	"path/filepath"
	"testing"
)

// The two cases that made the board disagree with the gwt ledger about what a
// session is called.

// A session started inside a subdirectory belongs to the repository, not to
// the subdirectory. This is the one that turned `dotfiles` into `powershell`.
func TestASubdirectoryBelongsToItsRepo(t *testing.T) {
	repo := t.TempDir()
	writeGitDir(t, filepath.Join(repo, ".git"), "ref: refs/heads/main\n")
	sub := filepath.Join(repo, "powershell", "onpath")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got := ReadGitInfo(sub)
	if got.Repo != filepath.Base(repo) {
		t.Fatalf("repo is %q, wanted %q", got.Repo, filepath.Base(repo))
	}
	if got.Branch != "main" {
		t.Fatalf("branch is %q, wanted main", got.Branch)
	}
}

// A linked worktree's directory is named for its BRANCH, so naming a card
// after it loses which repository it is. Two worktrees of one repo became
// `2026-08-21` and `aug-21-2026` with nothing tying either to `ziti-tv`.
func TestAWorktreeKnowsItsRepo(t *testing.T) {
	root := t.TempDir()

	// The real repository, with the worktree's git directory inside it.
	repo := filepath.Join(root, "ziti-tv")
	wtGit := filepath.Join(repo, ".git", "worktrees", "aug-21-2026")
	writeGitDir(t, wtGit, "ref: refs/heads/aug-21-2026\n")

	// The worktree itself, elsewhere, with a .git FILE pointing back.
	wt := filepath.Join(root, "worktrees", "aug-21-2026")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"),
		[]byte("gitdir: "+filepath.ToSlash(wtGit)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := ReadGitInfo(wt)
	if got.Repo != "ziti-tv" {
		t.Fatalf("repo is %q, wanted ziti-tv", got.Repo)
	}
	if got.Branch != "aug-21-2026" {
		t.Fatalf("branch is %q, wanted aug-21-2026", got.Branch)
	}
}

// A branch with a slash in it is one branch, not a path.
func TestABranchCanContainASlash(t *testing.T) {
	repo := t.TempDir()
	writeGitDir(t, filepath.Join(repo, ".git"), "ref: refs/heads/feature/nested/thing\n")

	if got := ReadGitInfo(repo).Branch; got != "feature/nested/thing" {
		t.Fatalf("branch is %q, wanted feature/nested/thing", got)
	}
}

// A detached head has no branch. That is a state, not a failure, and the
// repository is still known.
func TestADetachedHeadHasNoBranch(t *testing.T) {
	repo := t.TempDir()
	writeGitDir(t, filepath.Join(repo, ".git"),
		"9fceb02d0ae598e95dc970b74767f19372d61af8\n")

	got := ReadGitInfo(repo)
	if got.Branch != "" {
		t.Fatalf("a detached head reported branch %q", got.Branch)
	}
	if got.Repo != filepath.Base(repo) {
		t.Fatalf("repo is %q, wanted %q", got.Repo, filepath.Base(repo))
	}
}

// A directory that is in no repository answers nothing rather than walking to
// the root and inventing something.
func TestNoRepoAnswersNothing(t *testing.T) {
	got := ReadGitInfo(t.TempDir())
	if got.Repo != "" || got.Branch != "" {
		t.Fatalf("a directory outside a repo answered %+v", got)
	}
}

// An empty path is asked about constantly, since a card can have no worktree.
func TestNoPathAnswersNothing(t *testing.T) {
	if got := ReadGitInfo(""); got.Repo != "" || got.Branch != "" {
		t.Fatalf("an empty path answered %+v", got)
	}
}

func writeGitDir(t *testing.T, dir, head string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}
}
