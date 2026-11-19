//go:build integration

package store

import (
	"os"
	"path/filepath"
	"testing"
)

func writeGitConfig(t *testing.T, gitdir, config string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(gitdir, "config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOriginIsReadFromTheCheckoutsRemotes(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, config, want string }{
		{"origin wins", "[core]\n\tbare = false\n[remote \"hub\"]\n\turl = http://h/git/hub/github/o/r.git\n" +
			"[remote \"origin\"]\n\turl = git@github.com:o/r.git\n", "git@github.com:o/r.git"},
		{"hub without origin", "[remote \"hub\"]\n\turl = http://127.0.0.1:7777/git/hub/github/o/r.git\n",
			"http://127.0.0.1:7777/git/hub/github/o/r.git"},
		{"first otherwise", "[remote \"up\"]\n\turl = https://gitlab.com/a/b\n[remote \"fork\"]\n\turl = https://x/y/z\n",
			"https://gitlab.com/a/b"},
		{"no remote", "[core]\n\tbare = false\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := t.TempDir()
			gitdir := filepath.Join(repo, ".git")
			writeGitDir(t, gitdir, "ref: refs/heads/main\n")
			writeGitConfig(t, gitdir, c.config)
			if got := ReadGitInfo(repo).Origin; got != c.want {
				t.Fatalf("origin is %q, wanted %q", got, c.want)
			}
		})
	}
}

func TestALinkedWorktreeReadsTheSharedConfig(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repo := filepath.Join(root, "atrium")
	writeGitDir(t, filepath.Join(repo, ".git"), "ref: refs/heads/main\n")
	writeGitConfig(t, filepath.Join(repo, ".git"), "[remote \"origin\"]\n\turl = https://github.com/o/atrium.git\n")
	wtGit := filepath.Join(repo, ".git", "worktrees", "feature")
	writeGitDir(t, wtGit, "ref: refs/heads/feature\n")
	if err := os.WriteFile(filepath.Join(wtGit, "commondir"), []byte("../..\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(root, "feature")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+filepath.ToSlash(wtGit)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ReadGitInfo(wt).Origin; got != "https://github.com/o/atrium.git" {
		t.Fatalf("origin is %q", got)
	}
}

func TestBackfillFillsTheOrigin(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	repo := t.TempDir()
	card, _, err := s.Register(Observed{WireName: "o", Worktree: repo})
	if err != nil {
		t.Fatal(err)
	}
	if card.Origin != "" {
		t.Fatalf("origin before git is %q", card.Origin)
	}
	writeGitDir(t, filepath.Join(repo, ".git"), "ref: refs/heads/main\n")
	writeGitConfig(t, filepath.Join(repo, ".git"), "[remote \"origin\"]\n\turl = git@github.com:o/r.git\n")
	if _, err := s.BackfillGitInfo(); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Origin != "git@github.com:o/r.git" {
		t.Fatalf("origin after backfill is %q", got.Origin)
	}
}
