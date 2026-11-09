package daemon

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

func TestForkArgsPutForkSessionAfterTheResumeArgsAndKeepThePrompt(t *testing.T) {
	h := &store.Harness{Label: "claude", Cmd: "claude", Args: []string{"--base"},
		ResumeArgs: []string{"--resume", "{resume}", "--strict-mcp-config"},
		ModelArgs:  []string{"--model", "{model}"}, PromptArgs: []string{"{prompt}"}}
	args, logged, err := forkArgs(h, "conv-1", "carry on", launchOptions{Model: "opus", Args: []string{"--verbose"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--resume", "conv-1", "--strict-mcp-config", "--fork-session", "--model", "opus", "--verbose", "carry on"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %q, want %q", args, want)
	}
	if strings.Contains(logged, "carry on") || !strings.Contains(logged, "--fork-session") {
		t.Fatalf("logged = %q: want the fork and not the prompt", logged)
	}
	if args, _, err = forkArgs(h, "conv-1", "", launchOptions{}); err != nil || args[len(args)-1] != "--fork-session" {
		t.Fatalf("no prompt: args = %q, err %v", args, err)
	}
}

func TestForkArgsRefuseAPromptARunnerCannotTake(t *testing.T) {
	h := &store.Harness{Label: "claude", Cmd: "claude", ResumeArgs: []string{"--resume", "{resume}"}}
	if _, _, err := forkArgs(h, "conv-1", "hi", launchOptions{}); err == nil {
		t.Fatal("a prompt with no prompt arguments was not refused")
	}
}

func TestCopySessionPutsTheTranscriptWhereClaudeLooksFromTheNewFolderAndLeavesTheOriginal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	from, to := filepath.Join(home, "cards", "orig"), filepath.Join(home, "cards", "clone.one")
	src, err := api.ProjectDirFor(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"type":"user"}` + "\n")
	if err := os.WriteFile(filepath.Join(src, "6f1c2a9e.jsonl"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copySession(from, to, "6f1c2a9e"); err != nil {
		t.Fatal(err)
	}
	dst, _ := api.ProjectDirFor(to)
	if got, err := os.ReadFile(filepath.Join(dst, "6f1c2a9e.jsonl")); err != nil || string(got) != string(body) {
		t.Fatalf("copied transcript = %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(src, "6f1c2a9e.jsonl")); err != nil || string(got) != string(body) {
		t.Fatalf("the original transcript changed: %q, %v", got, err)
	}
	for _, conv := range []string{"../escape", "", "missing"} {
		if err := copySession(from, to, conv); err == nil {
			t.Errorf("copySession(%q) was not refused", conv)
		}
	}
}

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
