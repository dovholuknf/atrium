//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCarryChangesAppliesTheDiffAndCopiesUntrackedFiles(t *testing.T) {
	root := t.TempDir()
	repo, wt, tmp := filepath.Join(root, "repo"), filepath.Join(root, "wt"), filepath.Join(root, "tmp")
	for _, d := range []string{repo, tmp} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		out, err := gitIn(dir, append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "core.autocrlf=false"}, args...)...)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return out
	}
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(repo, "init", "-q")
	write(filepath.Join(repo, "a.txt"), "one\n")
	write(filepath.Join(repo, "gone.txt"), "bye\n")
	git(repo, "add", ".")
	git(repo, "commit", "-qm", "first")
	head := git(repo, "rev-parse", "HEAD")
	write(filepath.Join(repo, "a.txt"), "one\ntwo\n")
	if err := os.Remove(filepath.Join(repo, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(repo, "new", "b.txt"), "fresh\n")
	git(repo, "worktree", "add", "-q", "-b", "clone", wt, head)

	if err := carryChanges(repo, wt, tmp); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"a.txt": "one\ntwo\n", "new/b.txt": "fresh\n"} {
		if got, err := os.ReadFile(filepath.Join(wt, name)); err != nil || string(got) != want {
			t.Errorf("%s in the clone = %q, %v, want %q", name, got, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(wt, "gone.txt")); !os.IsNotExist(err) {
		t.Errorf("a file deleted in the original is still in the clone: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(repo, "a.txt")); string(got) != "one\ntwo\n" {
		t.Errorf("the original's change was touched: %q", got)
	}
	if _, err := os.Stat(filepath.Join(tmp, "clone.patch")); !os.IsNotExist(err) {
		t.Errorf("the patch was left behind: %v", err)
	}
	files, err := dirtyFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(files, ",") != "a.txt,gone.txt,new/b.txt" {
		t.Errorf("dirtyFiles = %q", files)
	}
}
