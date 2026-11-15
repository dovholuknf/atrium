//go:build integration

package daemon

import (
	"errors"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func stubCommits(t *testing.T, d *Daemon, inDir, onHub bool) {
	t.Helper()
	d.hk.commitInDir = func(string, string) bool { return inDir }
	d.hk.commitOnHub = func(hubAsker, *store.Task, string) bool { return onHub }
}

func TestAnEndingDoneWithAnUnknownCommitIsRefusedAndRecordsNothing(t *testing.T) {
	d := testDaemon(t)
	_, worker := launchedPair(t, d)
	stubCommits(t, d, false, false)
	rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", Ended: true})
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "abc1234") {
		t.Fatalf("code %d, body %s", rec.Code, rec.Body)
	}
	got, _ := d.st.Get(worker.ID)
	if got.ReportedAt != nil || got.Status == "done" {
		t.Fatalf("a refused ending was recorded: %+v", got)
	}
}

func TestAnEndingDoneWithABadShapeIsRefused(t *testing.T) {
	d := testDaemon(t)
	launchedPair(t, d)
	stubCommits(t, d, true, true)
	if rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "nothex!", Ended: true}); rec.Code != 400 {
		t.Fatalf("code %d", rec.Code)
	}
}

func TestAnEndingDoneTakesACommitTheHubHasAndTellsTheLauncherWithNoNudge(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	stubCommits(t, d, false, true)
	rec, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234",
		Recap: "done abc1234", Ended: true})
	if rec.Code != 200 || out["launcher_told"] != "queued" {
		t.Fatalf("code %d, out %v", rec.Code, out)
	}
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "done abc1234") {
		t.Fatalf("the launcher has %+v", msgs)
	}
	stopSilently(t, d, "worker")
	if n := len(pendingFrom(t, d, worker.ID)); n != 0 {
		t.Fatalf("the worker was nudged after done: %d", n)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("the launcher heard %d, want only the report", n)
	}
}

func TestAnEndingBlockedRefusesNoReasonAndOver50WordsAndDeliversTheReasonWithNoNudge(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	for _, why := range []string{"", " \n ", strings.Repeat("w ", 51)} {
		if rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportBlocked, Ask: why, Ended: true}); rec.Code != 400 {
			t.Fatalf("%q: code %d", why, rec.Code)
		}
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 0 {
		t.Fatalf("a refused ending told the launcher: %d", n)
	}
	reason := "need a token\nfrom the operator"
	if rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportBlocked, Ask: reason, Ended: true}); rec.Code != 200 {
		t.Fatalf("code %d, %s", rec.Code, rec.Body)
	}
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "blocked") || !strings.Contains(msgs[0].Text, "from the operator") {
		t.Fatalf("the launcher has %+v", msgs)
	}
	stopSilently(t, d, "worker")
	if n := len(pendingFrom(t, d, worker.ID)); n != 0 {
		t.Fatalf("the worker was nudged after blocked: %d", n)
	}
	w, err := d.st.WorkItem(worker.ID)
	if err == nil && w.State != "reported" {
		t.Fatalf("the work item is %q, want reported", w.State)
	}
}

func TestAnEndingDoneWithACommitOnlyHerePushesTheBranchAndTellsTheLauncherItIsOnTheHub(t *testing.T) {
	d := testDaemon(t)
	launcher, _ := launchedPair(t, d)
	stubCommits(t, d, true, false)
	pushed := 0
	d.hk.pushBranch = func(*Daemon, *store.Task) (string, error) { pushed++; return "claude/fix-x", nil }
	rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", Recap: "done abc1234", Ended: true})
	if rec.Code != 200 || pushed != 1 {
		t.Fatalf("code %d, pushed %d, body %s", rec.Code, pushed, rec.Body)
	}
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "on the hub: claude/fix-x at abc1234") {
		t.Fatalf("the launcher has %+v", msgs)
	}
}

func TestAnEndingDoneWhosePushFailsIsRefusedAndRecordsNothing(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	stubCommits(t, d, true, false)
	d.hk.pushBranch = func(*Daemon, *store.Task) (string, error) { return "", errors.New("remote: atrium: refused") }
	rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", Ended: true})
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "remote: atrium: refused") {
		t.Fatalf("code %d, body %s", rec.Code, rec.Body)
	}
	if got, _ := d.st.Get(worker.ID); got.ReportedAt != nil || got.Status == "done" {
		t.Fatalf("a refused ending was recorded: %+v", got)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 0 {
		t.Fatalf("a refused ending told the launcher: %d", n)
	}
}

func TestASpikesDoneNeedsItsFollowUpsAndAWriteupOnTheHub(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	if err := d.st.SetTags(worker.ID, []string{"sdk", OriginAgentTag, WorkTagPrefix + "s-any-runner"}); err != nil {
		t.Fatal(err)
	}
	stubCommits(t, d, false, true)
	for _, f := range []string{"", " ", "not an id!"} {
		rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", FollowUps: f, Ended: true})
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), "followups") {
			t.Fatalf("%q: code %d, body %s", f, rec.Code, rec.Body)
		}
	}
	if rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, Artifact: "spike/WRITEUP.md",
		FollowUps: "none", Ended: true}); rec.Code != 400 || !strings.Contains(rec.Body.String(), "on the hub") {
		t.Fatalf("a writeup only on the room: code %d, body %s", rec.Code, rec.Body)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 0 {
		t.Fatalf("a refused ending told the launcher: %d", n)
	}
	rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234",
		FollowUps: "f-one, f-two", Ended: true})
	if rec.Code != 200 {
		t.Fatalf("code %d, body %s", rec.Code, rec.Body)
	}
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "follow-ups: f-one, f-two") || !strings.Contains(msgs[0].Text, "on the hub: abc1234") {
		t.Fatalf("the launcher has %+v", msgs)
	}
}

// doneHubRig is a worker card on its own branch with one commit, a hub remote on this room's forwarder, and the
// forwarder being git's own http-backend over a bare repository, so done's push is the real one.
func doneHubRig(t *testing.T, d *Daemon, rejectPush bool) (launcher, worker *store.Task, bare, tip string) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	launcher, worker, _, dir := visibleFixture(t, d)
	bare = filepath.Join(t.TempDir(), "hub", "github", "o", "r.git")
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		t.Fatal(err)
	}
	visGit(t, filepath.Dir(bare), "init", "-q", "--bare", bare)
	visGit(t, bare, "config", "http.receivepack", "true")
	if rejectPush {
		hook := "#!/bin/sh\necho 'atrium: this hub takes no pushes'\nexit 1\n"
		if err := os.WriteFile(filepath.Join(bare, "hooks", "pre-receive"), []byte(hook), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	execPath, _ := exec.Command(gitPath, "--exec-path").Output()
	backend := filepath.Join(strings.TrimSpace(string(execPath)), "git-http-backend")
	srv := httptest.NewServer(&cgi.Handler{Path: backend, Root: "/git/", Env: []string{
		"GIT_PROJECT_ROOT=" + filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(bare)))), "GIT_HTTP_EXPORT_ALL=1",
		"GIT_HTTP_RECEIVE_PACK=true"}})
	t.Cleanup(srv.Close)
	d.SetAgentAddr(srv.Listener.Addr())
	d.SetHubGit(func() (http.RoundTripper, error) { return http.DefaultTransport, nil })
	visGit(t, dir, "remote", "add", "hub", d.HubRemoteBase()+"hub/github/o/r.git")
	visGit(t, dir, "commit", "-q", "--allow-empty", "-m", "the work")
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	tip = strings.TrimSpace(string(out))
	if err := d.st.SetStatus(worker.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLineage(worker.ID, launcher.WireName, launcher.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetTags(worker.ID, []string{"sdk", OriginAgentTag}); err != nil {
		t.Fatal(err)
	}
	d.hk.commitOnHub = func(hubAsker, *store.Task, string) bool { return false }
	return launcher, worker, bare, tip
}

func TestDonePushesAnUnpushedBranchToTheHubForReal(t *testing.T) {
	d := testDaemon(t)
	launcher, _, bare, tip := doneHubRig(t, d, false)
	rec, _ := finishWith(t, d, FinishRequest{Agent: "helper", Status: ReportDone, SHA: tip, Recap: "done", Ended: true})
	if rec.Code != 200 {
		t.Fatalf("code %d, body %s", rec.Code, rec.Body)
	}
	out, err := exec.Command("git", "--git-dir", bare, "rev-parse", "--verify", "refs/heads/claude/x").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != tip {
		t.Fatalf("the hub has claude/x at %q (%v), want %s", out, err, tip)
	}
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "on the hub: claude/x at "+tip) {
		t.Fatalf("the launcher has %+v", msgs)
	}
}

func TestDoneIsRefusedWhenTheHubRejectsThePush(t *testing.T) {
	d := testDaemon(t)
	launcher, worker, bare, tip := doneHubRig(t, d, true)
	rec, _ := finishWith(t, d, FinishRequest{Agent: "helper", Status: ReportDone, SHA: tip, Recap: "done", Ended: true})
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "this hub takes no pushes") {
		t.Fatalf("code %d, body %s", rec.Code, rec.Body)
	}
	if out, err := exec.Command("git", "--git-dir", bare, "rev-parse", "--verify", "refs/heads/claude/x").CombinedOutput(); err == nil {
		t.Fatalf("the rejected branch is on the hub: %s", out)
	}
	if got, _ := d.st.Get(worker.ID); got.ReportedAt != nil || got.Status == "done" {
		t.Fatalf("a refused ending was recorded: %+v", got)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 0 {
		t.Fatalf("a refused ending told the launcher: %d", n)
	}
}
