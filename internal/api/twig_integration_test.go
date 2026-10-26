//go:build integration

package api

import (
	"os"
	"path/filepath"
	"testing"
)

// A TWIG STARTS AT THE WORKTREE'S LOCAL HEAD, not origin's, and carries its tracked changes unstaged and its untracked
// files and folders. The worktree it came from keeps its work, and a name that is already taken is refused.
func TestTwigBranchesOffLocalHeadAndCarriesUncommittedWork(t *testing.T) {
	nh := newNewWorkHarness(t)
	code, out := nh.newWork(t, map[string]any{"name": "src"})
	if code != 201 {
		t.Fatalf("%d %v", code, out)
	}
	src := out["worktree"].(string)
	write := func(dir, rel, body string) {
		t.Helper()
		p := filepath.Join(filepath.FromSlash(dir), filepath.FromSlash(rel))
		mkDir(t, filepath.Dir(p))
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(src, "tracked.txt", "one\n")
	write(src, "bin.dat", "\x00\x01\x02")
	git(t, src, "add", "tracked.txt", "bin.dat")
	git(t, src, "commit", "-q", "-m", "local only")
	head := git(t, src, "rev-parse", "HEAD")
	write(src, "tracked.txt", "two\n")
	write(src, "bin.dat", "\x00\x03\x04")
	write(src, "loose.txt", "loose\n")
	write(src, "newdir/deep/file.txt", "deep\n")

	n := len(nh.launched)
	code, out = nh.newWork(t, map[string]any{"name": "tangent", "twig": src})
	if code != 201 || out["patch"] != nil || out["carry_error"] != nil || len(nh.launched) != n+1 {
		t.Fatalf("%d %v", code, out)
	}
	dest := out["worktree"].(string)
	if got := git(t, dest, "rev-parse", "HEAD"); got != head {
		t.Errorf("the twig is at %s, the worktree's HEAD is %s", got, head)
	}
	if got := git(t, dest, "diff", "--cached", "--name-only"); got != "" {
		t.Errorf("the carried diff is staged: %q", got)
	}
	for rel, want := range map[string]string{"tracked.txt": "two\n", "bin.dat": "\x00\x03\x04", "loose.txt": "loose\n",
		"newdir/deep/file.txt": "deep\n"} {
		if b, err := os.ReadFile(filepath.Join(filepath.FromSlash(dest), filepath.FromSlash(rel))); err != nil || string(b) != want {
			t.Errorf("%s in the twig is %q %v", rel, b, err)
		}
	}
	if got := git(t, src, "status", "--porcelain"); got == "" {
		t.Error("the worktree twigged from lost its work")
	}

	for _, name := range []string{"tangent", "src", "main"} {
		code, out = nh.newWork(t, map[string]any{"name": name, "twig": src})
		if code != 409 || len(nh.launched) != n+1 {
			t.Errorf("a twig named %s: %d %v", name, code, out)
		}
	}
}
