//go:build integration && git

package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func stampRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		full := append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)
		c := exec.Command("git", full...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "claude/item")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "one")
	return dir
}

func TestStampReadsTheWorktreeAndKeepsTheLogOutsideIt(t *testing.T) {
	dir := stampRepo(t)
	state := t.TempDir()
	task := &store.Task{Worktree: dir}
	rep, err := stamp(state, task, stampRequest{Item: "f-x", Log: "ok  all passed\n"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.AtSHA) != 40 || rep.Branch != "claude/item" || rep.Dirty {
		t.Fatalf("stamp = %+v", rep)
	}
	if !strings.HasPrefix(rep.LogPath, filepath.ToSlash(state)) || strings.HasPrefix(rep.LogPath, filepath.ToSlash(dir)) {
		t.Fatalf("the log is kept outside the repo, in the state dir: %q", rep.LogPath)
	}
	if b, err := os.ReadFile(filepath.FromSlash(rep.LogPath)); err != nil || !strings.Contains(string(b), "all passed") {
		t.Fatalf("log: %q %v", b, err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(filepath.FromSlash(rep.LogPath)); fi.Mode().Perm() != 0o600 {
			t.Fatalf("log mode %v", fi.Mode().Perm())
		}
	}

	// atrium's own brief does not make a tree dirty, a file of the card's does
	_ = os.WriteFile(filepath.Join(dir, "BRIEF.md"), []byte("x"), 0o644)
	if rep, _ = stamp(state, task, stampRequest{Item: "f-x"}); rep.Dirty {
		t.Fatal("BRIEF.md made the tree dirty")
	}
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed"), 0o644)
	if rep, _ = stamp(state, task, stampRequest{Item: "f-x"}); !rep.Dirty {
		t.Fatal("an edited file did not make the tree dirty")
	}
}

func TestStampRefusesALogFileOutsideTheCardAndABadItem(t *testing.T) {
	dir := stampRepo(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	_ = os.WriteFile(outside, []byte("secret"), 0o600)
	task := &store.Task{Worktree: dir}
	if _, err := stamp(t.TempDir(), task, stampRequest{Item: "f-x", LogFile: outside}); err == nil {
		t.Fatal("a log file outside the card's directory was read")
	}
	if _, err := stamp(t.TempDir(), task, stampRequest{Item: "../evil"}); err == nil {
		t.Fatal("an item id with .. was taken")
	}
	if _, err := stamp(t.TempDir(), &store.Task{}, stampRequest{Item: "f-x"}); err == nil {
		t.Fatal("a card with no directory was stamped")
	}
}

func TestDirtyStatus(t *testing.T) {
	for in, want := range map[string]bool{
		"":                          false,
		"?? BRIEF.md":               false,
		"?? BRIEF.md\n?? REPORT.md": false,
		" M a.go":                   true,
		"?? BRIEF.md\n?? new.go":    true,
		"?? \"BRIEF.md\"\r\n":       false,
	} {
		if got := dirtyStatus(in); got != want {
			t.Errorf("dirtyStatus(%q) = %v", in, got)
		}
	}
}
