//go:build integration

package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/home"
	"net/http/cgi"
	"net/http/httptest"
	"os"
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
	l := d.visibleLauncher(t.Context(), worker, "", launcher.WireName, "", dir)
	if l == nil || l.ID != launcher.ID {
		t.Fatalf("the launcher was not found as sharing the repository: %v", l)
	}
	if l := d.visibleLauncher(t.Context(), worker, launcher.ID, "", "", dir); l == nil {
		t.Fatal("not found by card id")
	}
	if l := d.visibleLauncher(t.Context(), worker, "", stranger.WireName, "", dir); l != nil {
		t.Fatalf("a launcher on another repository was said to see the commit: %v", l.WireName)
	}
	if l := d.visibleLauncher(t.Context(), worker, "", "nobody", "", dir); l != nil {
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

func TestGitPushReachesThisRoomsForwarderWhenTheHubRemoteIsAnotherRoomsAndTheLauncherSharesTheRepo(t *testing.T) {
	d := testDaemon(t)
	launcher, worker, _, dir := visibleFixture(t, d)
	// this room's forwarder: git's own http-backend over a bare repository, the hub's copy of o/r
	bare := filepath.Join(t.TempDir(), "hub", "github", "o", "r.git")
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		t.Fatal(err)
	}
	visGit(t, filepath.Dir(bare), "init", "-q", "--bare", bare)
	visGit(t, bare, "config", "http.receivepack", "true")
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	execPath, _ := exec.Command(gitPath, "--exec-path").Output()
	backend := filepath.Join(strings.TrimSpace(string(execPath)), "git-http-backend")
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(bare))))
	srv := httptest.NewServer(&cgi.Handler{Path: backend, Root: "/git/", Env: []string{
		"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1", "GIT_HTTP_RECEIVE_PACK=true"}})
	defer srv.Close()
	d.SetAgentAddr(srv.Listener.Addr())
	visGit(t, launcher.Worktree, "remote", "add", "origin", "https://github.com/o/r.git")
	const other = "http://127.0.0.1:8888/git/hub/github/o/r.git"
	visGit(t, launcher.Worktree, "remote", "add", "hub", other)
	if err := d.st.SetSetting(store.SettingGitPush, "hub"); err != nil {
		t.Fatal(err)
	}
	tok, err := d.HubGit.Cards.Mint(worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	d.HubGit.Mu.Lock()
	if d.HubGit.Issued == nil {
		d.HubGit.Issued = map[string]home.IssuedToken{}
	}
	d.HubGit.Issued[worker.ID] = home.IssuedToken{Tok: tok}
	d.HubGit.Mu.Unlock()
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
	rep, _ := got.(map[string]any)["report"].(string)
	if strings.Contains(rep, "nothing to push") || !strings.Contains(rep, "boss sees this commit already on disk") {
		t.Fatalf("report %q", rep)
	}
	ref := exec.Command("git", "--git-dir", bare, "rev-parse", "--verify", "refs/heads/claude/x")
	if out, err := ref.CombinedOutput(); err != nil {
		t.Fatalf("the branch did not reach this room's forwarder: %v\n%s", err, out)
	}
	rows, _ := d.st.Resources(worker.ID)
	found := false
	for _, r := range rows {
		found = found || r.Kind == store.ResHubBranch && r.Ref == "claude/x"
	}
	if !found {
		t.Fatal("the hub branch was not recorded")
	}
	url := exec.Command("git", "config", "--get", "remote.hub.url")
	url.Dir = dir
	if out, _ := url.Output(); strings.TrimSpace(string(out)) != other {
		t.Fatalf("the shared config changed: remote.hub.url = %q", out)
	}
}

func TestALauncherOnAnotherRoomIsFoundByItsDirectory(t *testing.T) {
	d := testDaemon(t)
	launcher, worker, stranger, dir := visibleFixture(t, d)
	// not in this store: the launcher's card lives on another room, so only its directory crosses
	l := d.visibleLauncher(t.Context(), worker, "claude-sg4~abc", "boss@sg4-control", launcher.Worktree, dir)
	if l == nil || l.WireName != "boss@sg4-control" {
		t.Fatalf("a launcher on another room in the same repository was not found: %v", l)
	}
	if l := d.visibleLauncher(t.Context(), worker, "claude-sg4~abc", "boss@sg4-control", stranger.Worktree, dir); l != nil {
		t.Fatalf("a launcher in another repository was said to see the commit: %v", l.WireName)
	}
	if l := d.visibleLauncher(t.Context(), worker, "", "boss@sg4-control", "", dir); l != nil {
		t.Fatal("no directory was still said to see the commit")
	}
	if l := d.visibleLauncher(t.Context(), worker, "", "boss@sg4-control", filepath.Join(dir, "missing"), dir); l != nil {
		t.Fatal("a directory that is not there was said to see the commit")
	}
	if l := d.visibleLauncher(t.Context(), worker, "", "ghost", launcher.Worktree, dir); l != nil {
		t.Fatal("a name with no room was trusted for its directory")
	}
}

func TestTheCardKeepsALauncherDirectoryFromAnotherRoom(t *testing.T) {
	d := testDaemon(t)
	_, worker, _, _ := visibleFixture(t, d)
	if err := d.st.SetSpawnedByDir(worker.ID, "  /srv/work/y "); err != nil {
		t.Fatal(err)
	}
	got, err := d.st.Get(worker.ID)
	if err != nil || got.SpawnedByDir != "/srv/work/y" {
		t.Fatalf("not kept: %v %q", err, got.SpawnedByDir)
	}
}
