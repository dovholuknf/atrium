//go:build integration

package daemon

import (
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func visGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// a launcher card on a checkout, a worker card on a worktree of it, and a worker on an unrelated repository
func visibleFixture(t *testing.T, d *Daemon) (launcher, worker, stranger *store.Task, workerDir string) {
	t.Helper()
	root := t.TempDir()
	checkout := filepath.Join(root, "checkout")
	workerDir = filepath.Join(root, "worktree")
	visGit(t, root, "init", "-q", checkout)
	visGit(t, checkout, "commit", "-q", "--allow-empty", "-m", "first")
	visGit(t, checkout, "worktree", "add", "-q", "-b", "claude/x", workerDir)
	elsewhere := filepath.Join(root, "elsewhere")
	visGit(t, root, "init", "-q", elsewhere)
	reg := func(name, dir string) *store.Task {
		task, _, err := d.st.Register(store.Observed{WireName: name, Worktree: dir, Runner: "claude"})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	launcher = reg("boss", checkout)
	worker = reg("helper", workerDir)
	stranger = reg("stranger", elsewhere)
	return
}

func TestTheLaunchTextSaysCommitsAreVisibleToALauncherOnTheSameRepository(t *testing.T) {
	d := testDaemon(t)
	launcher, worker, stranger, dir := visibleFixture(t, d)
	l := d.visibleLauncher(t.Context(), worker, "", launcher.WireName, dir)
	if l == nil || l.ID != launcher.ID {
		t.Fatalf("the launcher was not found as sharing the repository: %v", l)
	}
	if l := d.visibleLauncher(t.Context(), worker, launcher.ID, "", dir); l == nil {
		t.Fatal("not found by card id")
	}
	if l := d.visibleLauncher(t.Context(), worker, "", stranger.WireName, dir); l != nil {
		t.Fatalf("a launcher on another repository was said to see the commit: %v", l.WireName)
	}
	if l := d.visibleLauncher(t.Context(), worker, "", "nobody", dir); l != nil {
		t.Fatal("an unknown launcher was said to see the commit")
	}
	text := withCommitVisible(briefPrompt("do it"), commitVisibleLine(launcher.WireName))
	if !strings.HasSuffix(text, LaunchEndingLine) {
		t.Fatalf("the ending is not last:\n%s", text)
	}
	if !strings.Contains(text, "already visible to "+launcher.WireName) || !strings.Contains(text, "do not push") {
		t.Fatalf("no visibility sentence:\n%s", text)
	}
	if strings.Contains(briefPrompt("do it"), "already visible") {
		t.Fatal("the sentence is in the plain launch text")
	}
	if strings.Contains(text, "git push origin") {
		t.Fatal("the launch text suggests git push origin")
	}
}

func TestGitPushIsASuccessWhenTheHubRemoteIsAnotherRoomsAndTheLauncherSharesTheRepo(t *testing.T) {
	d := testDaemon(t)
	d.setAgentAddr(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7777})
	launcher, worker, _, dir := visibleFixture(t, d)
	visGit(t, launcher.Worktree, "remote", "add", "hub", "http://127.0.0.1:8888/git/hub/github/o/r.git")
	if err := d.st.SetSetting(store.SettingGitPush, "hub"); err != nil {
		t.Fatal(err)
	}
	tok, err := d.hubGit.cards.Mint(worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	d.hubGit.mu.Lock()
	if d.hubGit.issued == nil {
		d.hubGit.issued = map[string]issuedToken{}
	}
	d.hubGit.issued[worker.ID] = issuedToken{tok: tok}
	d.hubGit.mu.Unlock()
	if err := d.st.SetStatus(worker.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLineage(worker.ID, launcher.WireName, launcher.ID); err != nil {
		t.Fatal(err)
	}
	got, err := d.GitPush(worker.ID, "claude/x")
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if rep, _ := got.(map[string]any)["report"].(string); rep != "nothing to push: boss sees this commit already" {
		t.Fatalf("report %q", rep)
	}
	_ = dir
}
